//go:generate ../tools/cmd/genjws.sh

// Package jws implements the digital signature on JSON based data
// structures as described in https://tools.ietf.org/html/rfc7515
//
// If you do not care about the details, the only things that you
// would need to use are the following functions:
//
//	jws.Sign(payload, jws.WithKey(algorithm, key))
//	jws.Verify(serialized, jws.WithKey(algorithm, key))
//
// To sign, simply use `jws.Sign`. `payload` is a []byte buffer that
// contains whatever data you want to sign. `alg` is one of the
// jwa.SignatureAlgorithm constants from package jwa. For RSA and
// ECDSA family of algorithms, you will need to prepare a private key.
// For HMAC family, you just need a []byte value. The `jws.Sign`
// function will return the encoded JWS message on success.
//
// To verify, use `jws.Verify`. It will parse the `encodedjws` buffer
// and verify the result using `algorithm` and `key`. Upon successful
// verification, the original payload is returned, so you can work on it.
package jws

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/lestrrat-go/blackmagic"
	"github.com/lestrrat-go/jwx/v2/internal/base64"
	"github.com/lestrrat-go/jwx/v2/internal/json"
	"github.com/lestrrat-go/jwx/v2/internal/pool"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/x25519"
)

var registry = json.NewRegistry()

type payloadSigner struct {
	signer    Signer
	key       interface{}
	protected Headers
	public    Headers
}

func (s *payloadSigner) Sign(payload []byte) ([]byte, error) {
	return s.signer.Sign(payload, s.key)
}

func (s *payloadSigner) Algorithm() jwa.SignatureAlgorithm {
	return s.signer.Algorithm()
}

func (s *payloadSigner) ProtectedHeader() Headers {
	return s.protected
}

func (s *payloadSigner) PublicHeader() Headers {
	return s.public
}

var signers = make(map[jwa.SignatureAlgorithm]Signer)
var muSigner = &sync.Mutex{}

func removeSigner(alg jwa.SignatureAlgorithm) {
	muSigner.Lock()
	defer muSigner.Unlock()
	delete(signers, alg)
}

func makeSigner(alg jwa.SignatureAlgorithm, key interface{}, public, protected Headers) (*payloadSigner, error) {
	muSigner.Lock()
	signer, ok := signers[alg]
	if !ok {
		v, err := NewSigner(alg)
		if err != nil {
			muSigner.Unlock()
			return nil, fmt.Errorf(`failed to create payload signer: %w`, err)
		}
		signers[alg] = v
		signer = v
	}
	muSigner.Unlock()

	return &payloadSigner{
		signer:    signer,
		key:       key,
		public:    public,
		protected: protected,
	}, nil
}

const (
	fmtInvalid = 1 << iota
	fmtCompact
	fmtJSON
	fmtJSONPretty
	fmtMax
)

// silence linters
var _ = fmtInvalid
var _ = fmtMax

func validateKeyBeforeUse(key interface{}) error {
	jwkKey, ok := key.(jwk.Key)
	if !ok {
		converted, err := jwk.FromRaw(key)
		if err != nil {
			return fmt.Errorf(`could not convert key of type %T to jwk.Key for validation: %w`, key, err)
		}
		jwkKey = converted
	}
	return jwkKey.Validate()
}

// Sign generates a JWS message for the given payload and returns
// it in serialized form, which can be in either compact or
// JSON format. Default is compact.
//
// You must pass at least one key to `jws.Sign()` by using `jws.WithKey()`
// option.
//
//	jws.Sign(payload, jws.WithKey(alg, key))
//	jws.Sign(payload, jws.WithJSON(), jws.WithKey(alg1, key1), jws.WithKey(alg2, key2))
//
// Note that in the second example the `jws.WithJSON()` option is
// specified as well. This is because the compact serialization
// format does not support multiple signatures, and users must
// specifically ask for the JSON serialization format.
//
// Read the documentation for `jws.WithKey()` to learn more about the
// possible values that can be used for `alg` and `key`.
//
// You may create JWS messages with the "none" (jwa.NoSignature) algorithm
// if you use the `jws.WithInsecureNoSignature()` option. This option
// can be combined with one or more signature keys, as well as the
// `jws.WithJSON()` option to generate multiple signatures (though
// the usefulness of such constructs is highly debatable)
//
// Note that this library does not allow you to successfully call `jws.Verify()` on
// signatures with the "none" algorithm. To parse these, use `jws.Parse()` instead.
//
// If you want to use a detached payload, use `jws.WithDetachedPayload()` as
// one of the options. When you use this option, you must always set the
// first parameter (`payload`) to `nil`, or the function will return an error
//
// You may also want to look at how to pass protected headers to the
// signing process, as you will likely be required to set the `b64` field
// when using detached payload.
//
// Look for options that return `jws.SignOption` or `jws.SignVerifyOption`
// for a complete list of options that can be passed to this function.
func Sign(payload []byte, options ...SignOption) ([]byte, error) {
	format := fmtCompact
	var signers []*payloadSigner
	var detached bool
	var noneSignature *payloadSigner
	var validateKey bool
	for _, option := range options {
		//nolint:forcetypeassert
		switch option.Ident() {
		case identSerialization{}:
			format = option.Value().(int)
		case identInsecureNoSignature{}:
			data := option.Value().(*withInsecureNoSignature)
			// only the last one is used (we overwrite previous values)
			noneSignature = &payloadSigner{
				signer:    noneSigner{},
				protected: data.protected,
			}
		case identKey{}:
			data := option.Value().(*withKey)

			alg, ok := data.alg.(jwa.SignatureAlgorithm)
			if !ok {
				return nil, fmt.Errorf(`jws.Sign: expected algorithm to be of type jwa.SignatureAlgorithm but got (%[1]q, %[1]T)`, data.alg)
			}

			// No, we don't accept "none" here.
			if alg == jwa.NoSignature {
				return nil, fmt.Errorf(`jws.Sign: "none" (jwa.NoSignature) cannot be used with jws.WithKey`)
			}

			signer, err := makeSigner(alg, data.key, data.public, data.protected)
			if err != nil {
				return nil, fmt.Errorf(`jws.Sign: failed to create signer: %w`, err)
			}
			signers = append(signers, signer)
		case identDetachedPayload{}:
			detached = true
			if payload != nil {
				return nil, fmt.Errorf(`jws.Sign: payload must be nil when jws.WithDetachedPayload() is specified`)
			}
			payload = option.Value().([]byte)
		case identValidateKey{}:
			validateKey = option.Value().(bool)
		}
	}

	if noneSignature != nil {
		signers = append(signers, noneSignature)
	}

	lsigner := len(signers)
	if lsigner == 0 {
		return nil, fmt.Errorf(`jws.Sign: no signers available. Specify an alogirthm and akey using jws.WithKey()`)
	}

	// Design note: while we could have easily set format = fmtJSON when
	// lsigner > 1, I believe the decision to change serialization formats
	// must be explicitly stated by the caller. Otherwise, I'm pretty sure
	// there would be people filing issues saying "I get JSON when I expected
	// compact serialization".
	//
	// Therefore, instead of making implicit format conversions, we force the
	// user to spell it out as `jws.Sign(..., jws.WithJSON(), jws.WithKey(...), jws.WithKey(...))`
	if format == fmtCompact && lsigner != 1 {
		return nil, fmt.Errorf(`jws.Sign: cannot have multiple signers (keys) specified for compact serialization. Use only one jws.WithKey()`)
	}

	// Create a Message object with all the bits and bobs, and we'll
	// serialize it in the end
	var result Message

	result.payload = payload

	result.signatures = make([]*Signature, 0, len(signers))
	for i, signer := range signers {
		protected := signer.ProtectedHeader()
		if protected == nil {
			protected = NewHeaders()
		}

		if err := protected.Set(AlgorithmKey, signer.Algorithm()); err != nil {
			return nil, fmt.Errorf(`failed to set "alg" header: %w`, err)
		}

		if key, ok := signer.key.(jwk.Key); ok {
			if kid := key.KeyID(); kid != "" {
				if err := protected.Set(KeyIDKey, kid); err != nil {
					return nil, fmt.Errorf(`failed to set "kid" header: %w`, err)
				}
			}
		}
		sig := &Signature{
			headers:   signer.PublicHeader(),
			protected: protected,
			// cheat. FIXXXXXXMEEEEEE
			detached: detached,
		}

		if validateKey {
			if err := validateKeyBeforeUse(signer.key); err != nil {
				return nil, fmt.Errorf(`jws.Verify: %w`, err)
			}
		}
		_, _, err := sig.Sign(payload, signer.signer, signer.key)
		if err != nil {
			return nil, fmt.Errorf(`failed to generate signature for signer #%d (alg=%s): %w`, i, signer.Algorithm(), err)
		}

		result.signatures = append(result.signatures, sig)
	}

	switch format {
	case fmtJSON:
		return json.Marshal(result)
	case fmtJSONPretty:
		return json.MarshalIndent(result, "", "  ")
	case fmtCompact:
		// Take the only signature object, and convert it into a Compact
		// serialization format
		var compactOpts []CompactOption
		if detached {
			compactOpts = append(compactOpts, WithDetached(detached))
		}
		return Compact(&result, compactOpts...)
	default:
		return nil, fmt.Errorf(`jws.Sign: invalid serialization format`)
	}
}

var allowNoneWhitelist = jwk.WhitelistFunc(func(string) bool {
	return false
})

// Verify checks if the given JWS message is verifiable using `alg` and `key`.
// `key` may be a "raw" key (e.g. rsa.PublicKey) or a jwk.Key
//
// If the verification is successful, `err` is nil, and the content of the
// payload that was signed is returned. If you need more fine-grained
// control of the verification process, manually generate a
// `Verifier` in `verify` subpackage, and call `Verify` method on it.
// If you need to access signatures and JOSE headers in a JWS message,
// use `Parse` function to get `Message` object.
//
// Because the use of "none" (jwa.NoSignature) algorithm is strongly discouraged,
// this function DOES NOT consider it a success when `{"alg":"none"}` is
// encountered in the message (it would also be counterintuitive when the code says
// it _verified_ something when in fact it did no such thing). If you want to
// accept messages with "none" signature algorithm, use `jws.Parse` to get the
// raw JWS message.
func Verify(buf []byte, options ...VerifyOption) ([]byte, error) {
	var parseOptions []ParseOption
	var dst *Message
	var detachedPayload []byte
	var keyProviders []KeyProvider
	var keyUsed interface{}
	var validateKey bool

	ctx := context.Background()

	//nolint:forcetypeassert
	for _, option := range options {
		switch option.Ident() {
		case identMessage{}:
			dst = option.Value().(*Message)
		case identDetachedPayload{}:
			detachedPayload = option.Value().([]byte)
		case identKey{}:
			pair := option.Value().(*withKey)
			alg, ok := pair.alg.(jwa.SignatureAlgorithm)
			if !ok {
				return nil, fmt.Errorf(`WithKey() option must be specified using jwa.SignatureAlgorithm (got %T)`, pair.alg)
			}
			keyProviders = append(keyProviders, &staticKeyProvider{
				alg: alg,
				key: pair.key,
			})
		case identKeyProvider{}:
			keyProviders = append(keyProviders, option.Value().(KeyProvider))
		case identKeyUsed{}:
			keyUsed = option.Value()
		case identContext{}:
			//nolint:fatcontext
			ctx = option.Value().(context.Context)
		case identValidateKey{}:
			validateKey = option.Value().(bool)
		case identSerialization{}:
			parseOptions = append(parseOptions, option.(ParseOption))
		default:
			return nil, fmt.Errorf(`invalid jws.VerifyOption %q passed`, `With`+strings.TrimPrefix(fmt.Sprintf(`%T`, option.Ident()), `jws.ident`))
		}
	}

	if len(keyProviders) < 1 {
		return nil, fmt.Errorf(`jws.Verify: no key providers have been provided (see jws.WithKey(), jws.WithKeySet(), jws.WithVerifyAuto(), and jws.WithKeyProvider()`)
	}

	msg, err := Parse(buf, parseOptions...)
	if err != nil {
		return nil, fmt.Errorf(`failed to parse jws: %w`, err)
	}
	defer msg.clearRaw()

	if detachedPayload != nil {
		if len(msg.payload) != 0 {
			return nil, fmt.Errorf(`can't specify detached payload for JWS with payload`)
		}

		msg.payload = detachedPayload
	}

	// Pre-compute the base64 encoded version of payload
	var payload string
	if msg.b64 {
		payload = base64.EncodeToString(msg.payload)
	} else {
		payload = string(msg.payload)
	}

	verifyBuf := pool.GetBytesBuffer()
	defer pool.ReleaseBytesBuffer(verifyBuf)

	var errs []error
	for i, sig := range msg.signatures {
		verifyBuf.Reset()

		var encodedProtectedHeader string
		if rbp, ok := sig.protected.(interface{ rawBuffer() []byte }); ok {
			if raw := rbp.rawBuffer(); raw != nil {
				encodedProtectedHeader = base64.EncodeToString(raw)
			}
		}

		if encodedProtectedHeader == "" {
			protected, err := json.Marshal(sig.protected)
			if err != nil {
				return nil, fmt.Errorf(`failed to marshal "protected" for signature #%d: %w`, i+1, err)
			}

			encodedProtectedHeader = base64.EncodeToString(protected)
		}

		verifyBuf.WriteString(encodedProtectedHeader)
		verifyBuf.WriteByte('.')
		verifyBuf.WriteString(payload)

		for i, kp := range keyProviders {
			var sink algKeySink
			if err := kp.FetchKeys(ctx, &sink, sig, msg); err != nil {
				return nil, fmt.Errorf(`key provider %d failed: %w`, i, err)
			}

			for _, pair := range sink.list {
				// alg is converted here because pair.alg is of type jwa.KeyAlgorithm.
				// this may seem ugly, but we're trying to avoid declaring separate
				// structs for `alg jwa.KeyEncryptionAlgorithm` and `alg jwa.SignatureAlgorithm`
				//nolint:forcetypeassert
				alg := pair.alg.(jwa.SignatureAlgorithm)
				key := pair.key

				if validateKey {
					if err := validateKeyBeforeUse(key); err != nil {
						return nil, fmt.Errorf(`jws.Verify: %w`, err)
					}
				}
				verifier, err := NewVerifier(alg)
				if err != nil {
					return nil, fmt.Errorf(`failed to create verifier for algorithm %q: %w`, alg, err)
				}

				if err := verifier.Verify(verifyBuf.Bytes(), sig.signature, key); err != nil {
					errs = append(errs, err)
					continue
				}

				if keyUsed != nil {
					if err := blackmagic.AssignIfCompatible(keyUsed, key); err != nil {
						return nil, fmt.Errorf(`failed to assign used key (%T) to %T: %w`, key, keyUsed, err)
					}
				}

				if dst != nil {
					*(dst) = *msg
				}

				return msg.payload, nil
			}
		}
	}
	return nil, &verifyError{errs: errs}
}

type verifyError struct {
	// Note: when/if we can ditch Go < 1.20, we can change this to a simple
	// `err error`, where the value is the result of `errors.Join()`
	//
	// We also need to implement Unwrap:
	// func (e *verifyError) Unwrap() error {
	//	return e.err
	//}
	//
	// And finally, As() can go away
	errs []error
}

func (e *verifyError) Error() string {
	return `could not verify message using any of the signatures or keys`
}

func (e *verifyError) As(target interface{}) bool {
	for _, err := range e.errs {
		if errors.As(err, target) {
			return true
		}
	}
	return false
}

// IsVerificationError returns true if the error came from the verification part of the
// jws.Verify function, allowing you to check if the error is a result of actual
// verification failure.
//
// For example, if the error happened while fetching a key
// from a datasource, feeding that error should to this function return false, whereas
// a failure to compute the signature for whatever reason would be a verification error
// and returns true.
func IsVerificationError(err error) bool {
	var ve *verifyError
	return errors.As(err, &ve)
}

// get the value of b64 header field.
// If the field does not exist, returns true (default)
// Otherwise return the value specified by the header field.
func getB64Value(hdr Headers) bool {
	b64raw, ok := hdr.Get("b64")
	if !ok {
		return true // default
	}

	b64, ok := b64raw.(bool) // default
	if !ok {
		return false
	}
	return b64
}

// This is an "optimized" io.ReadAll(). It will attempt to read
// all of the contents from the reader IF the reader is of a certain
// concrete type.
func readAll(rdr io.Reader) ([]byte, bool) {
	switch rdr.(type) {
	case *bytes.Reader, *bytes.Buffer, *strings.Reader:
		data, err := io.ReadAll(rdr)
		if err != nil {
			return nil, false
		}
		return data, true
	default:
		return nil, false
	}
}

// Parse parses contents from the given source and creates a jws.Message
// struct. By default the input can be in either compact or full JSON serialization.
//
// You may pass `jws.WithJSON()` and/or `jws.WithCompact()` to specify
// explicitly which format to use. If neither or both is specified, the function
// will attempt to autodetect the format. If one or the other is specified,
// only the specified format will be attempted.
func Parse(src []byte, options ...ParseOption) (*Message, error) {
	var formats int
	for _, option := range options {
		//nolint:forcetypeassert
		switch option.Ident() {
		case identSerialization{}:
			switch option.Value().(int) {
			case fmtJSON:
				formats |= fmtJSON
			case fmtCompact:
				formats |= fmtCompact
			}
		}
	}

	// if format is 0 or both JSON/Compact, auto detect
	if v := formats & (fmtJSON | fmtCompact); v == 0 || v == fmtJSON|fmtCompact {
		for i := range src {
			r := rune(src[i])
			if r >= utf8.RuneSelf {
				r, _ = utf8.DecodeRune(src)
			}
			if !unicode.IsSpace(r) {
				if r == '{' {
					return parseJSON(src)
				}
				return parseCompact(src)
			}
		}
	} else if formats&fmtCompact == fmtCompact {
		return parseCompact(src)
	} else if formats&fmtJSON == fmtJSON {
		return parseJSON(src)
	}

	return nil, fmt.Errorf(`invalid byte sequence`)
}

// Parse parses contents from the given source and creates a jws.Message
// struct. The input can be in either compact or full JSON serialization.
func ParseString(src string) (*Message, error) {
	return Parse([]byte(src))
}

// Parse parses contents from the given source and creates a jws.Message
// struct. The input can be in either compact or full JSON serialization.
func ParseReader(src io.Reader) (*Message, error) {
	if data, ok := readAll(src); ok {
		return Parse(data)
	}

	rdr := bufio.NewReader(src)
	var first rune
	for {
		r, _, err := rdr.ReadRune()
		if err != nil {
			return nil, fmt.Errorf(`failed to read rune: %w`, err)
		}
		if !unicode.IsSpace(r) {
			first = r
			if err := rdr.UnreadRune(); err != nil {
				return nil, fmt.Errorf(`failed to unread rune: %w`, err)
			}

			break
		}
	}

	var parser func(io.Reader) (*Message, error)
	if first == '{' {
		parser = parseJSONReader
	} else {
		parser = parseCompactReader
	}

	m, err := parser(rdr)
	if err != nil {
		return nil, fmt.Errorf(`failed to parse jws message: %w`, err)
	}

	return m, nil
}

func parseJSONReader(src io.Reader) (result *Message, err error) {
	var m Message
	if err := json.NewDecoder(src).Decode(&m); err != nil {
		return nil, fmt.Errorf(`failed to unmarshal jws message: %w`, err)
	}
	return &m, nil
}

func parseJSON(data []byte) (result *Message, err error) {
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf(`failed to unmarshal jws message: %w`, err)
	}
	return &m, nil
}

const tokenDelim = "."

// SplitCompact splits a JWT and returns its three parts
// separately: protected headers, payload and signature.
func SplitCompact(src []byte) ([]byte, []byte, []byte, error) {
	protected, s, ok := bytes.Cut(src, []byte(tokenDelim))
	if !ok { // no period found
		return nil, nil, nil, fmt.Errorf(`invalid number of segments`)
	}
	payload, s, ok := bytes.Cut(s, []byte(tokenDelim))
	if !ok { // only one period found
		return nil, nil, nil, fmt.Errorf(`invalid number of segments`)
	}
	signature, _, ok := bytes.Cut(s, []byte(tokenDelim))
	if ok { // three periods found
		return nil, nil, nil, fmt.Errorf(`invalid number of segments`)
	}
	return protected, payload, signature, nil
}

// SplitCompactString splits a JWT and returns its three parts
// separately: protected headers, payload and signature.
func SplitCompactString(src string) ([]byte, []byte, []byte, error) {
	return SplitCompact([]byte(src))
}

// SplitCompactReader splits a JWT and returns its three parts
// separately: protected headers, payload and signature.
func SplitCompactReader(rdr io.Reader) ([]byte, []byte, []byte, error) {
	if data, ok := readAll(rdr); ok {
		return SplitCompact(data)
	}

	var protected []byte
	var payload []byte
	var signature []byte
	var periods int
	var state int

	buf := make([]byte, 4096)
	var sofar []byte

	for {
		// read next bytes
		n, err := rdr.Read(buf)
		// return on unexpected read error
		if err != nil && err != io.EOF {
			return nil, nil, nil, fmt.Errorf(`unexpected end of input: %w`, err)
		}

		// append to current buffer
		sofar = append(sofar, buf[:n]...)
		// loop to capture multiple '.' in current buffer
		for loop := true; loop; {
			var i = bytes.IndexByte(sofar, '.')
			if i == -1 && err != io.EOF {
				// no '.' found -> exit and read next bytes (outer loop)
				loop = false
				continue
			} else if i == -1 && err == io.EOF {
				// no '.' found -> process rest and exit
				i = len(sofar)
				loop = false
			} else {
				// '.' found
				periods++
			}

			// Reaching this point means we have found a '.' or EOF and process the rest of the buffer
			switch state {
			case 0:
				protected = sofar[:i]
				state++
			case 1:
				payload = sofar[:i]
				state++
			case 2:
				signature = sofar[:i]
			}
			// Shorten current buffer
			if len(sofar) > i {
				sofar = sofar[i+1:]
			}
		}
		// Exit on EOF
		if err == io.EOF {
			break
		}
	}
	if periods != 2 {
		return nil, nil, nil, fmt.Errorf(`invalid number of segments`)
	}

	return protected, payload, signature, nil
}

// parseCompactReader parses a JWS value serialized via compact serialization.
func parseCompactReader(rdr io.Reader) (m *Message, err error) {
	protected, payload, signature, err := SplitCompactReader(rdr)
	if err != nil {
		return nil, fmt.Errorf(`invalid compact serialization format: %w`, err)
	}
	return parse(protected, payload, signature)
}

func parseCompact(data []byte) (m *Message, err error) {
	protected, payload, signature, err := SplitCompact(data)
	if err != nil {
		return nil, fmt.Errorf(`invalid compact serialization format: %w`, err)
	}
	return parse(protected, payload, signature)
}

func parse(protected, payload, signature []byte) (*Message, error) {
	decodedHeader, err := base64.Decode(protected)
	if err != nil {
		return nil, fmt.Errorf(`failed to decode protected headers: %w`, err)
	}

	hdr := NewHeaders()
	if err := json.Unmarshal(decodedHeader, hdr); err != nil {
		return nil, fmt.Errorf(`failed to parse JOSE headers: %w`, err)
	}

	var decodedPayload []byte
	b64 := getB64Value(hdr)
	if !b64 {
		decodedPayload = payload
	} else {
		v, err := base64.Decode(payload)
		if err != nil {
			return nil, fmt.Errorf(`failed to decode payload: %w`, err)
		}
		decodedPayload = v
	}

	decodedSignature, err := base64.Decode(signature)
	if err != nil {
		return nil, fmt.Errorf(`failed to decode signature: %w`, err)
	}

	var msg Message
	msg.payload = decodedPayload
	msg.signatures = append(msg.signatures, &Signature{
		protected: hdr,
		signature: decodedSignature,
	})
	msg.b64 = b64
	return &msg, nil
}

// RegisterCustomField allows users to specify that a private field
// be decoded as an instance of the specified type. This option has
// a global effect.
//
// For example, suppose you have a custom field `x-birthday`, which
// you want to represent as a string formatted in RFC3339 in JSON,
// but want it back as `time.Time`.
//
// In that case you would register a custom field as follows
//
//	jwe.RegisterCustomField(`x-birthday`, timeT)
//
// Then `hdr.Get("x-birthday")` will still return an `interface{}`,
// but you can convert its type to `time.Time`
//
//	bdayif, _ := hdr.Get(`x-birthday`)
//	bday := bdayif.(time.Time)
func RegisterCustomField(name string, object interface{}) {
	registry.Register(name, object)
}

// Helpers for signature verification
var rawKeyToKeyType = make(map[reflect.Type]jwa.KeyType)
var keyTypeToAlgorithms = make(map[jwa.KeyType][]jwa.SignatureAlgorithm)

func init() {
	rawKeyToKeyType[reflect.TypeOf([]byte(nil))] = jwa.OctetSeq
	rawKeyToKeyType[reflect.TypeOf(ed25519.PublicKey(nil))] = jwa.OKP
	rawKeyToKeyType[reflect.TypeOf(rsa.PublicKey{})] = jwa.RSA
	rawKeyToKeyType[reflect.TypeOf((*rsa.PublicKey)(nil))] = jwa.RSA
	rawKeyToKeyType[reflect.TypeOf(ecdsa.PublicKey{})] = jwa.EC
	rawKeyToKeyType[reflect.TypeOf((*ecdsa.PublicKey)(nil))] = jwa.EC

	addAlgorithmForKeyType(jwa.OKP, jwa.EdDSA)
	for _, alg := range []jwa.SignatureAlgorithm{jwa.HS256, jwa.HS384, jwa.HS512} {
		addAlgorithmForKeyType(jwa.OctetSeq, alg)
	}
	for _, alg := range []jwa.SignatureAlgorithm{jwa.RS256, jwa.RS384, jwa.RS512, jwa.PS256, jwa.PS384, jwa.PS512} {
		addAlgorithmForKeyType(jwa.RSA, alg)
	}
	for _, alg := range []jwa.SignatureAlgorithm{jwa.ES256, jwa.ES384, jwa.ES512} {
		addAlgorithmForKeyType(jwa.EC, alg)
	}
}

func addAlgorithmForKeyType(kty jwa.KeyType, alg jwa.SignatureAlgorithm) {
	keyTypeToAlgorithms[kty] = append(keyTypeToAlgorithms[kty], alg)
}

// AlgorithmsForKey returns the possible signature algorithms that can
// be used for a given key. It only takes in consideration keys/algorithms
// for verification purposes, as this is the only usage where one may need
// dynamically figure out which method to use.
func AlgorithmsForKey(key interface{}) ([]jwa.SignatureAlgorithm, error) {
	var kty jwa.KeyType
	switch key := key.(type) {
	case jwk.Key:
		kty = key.KeyType()
	case rsa.PublicKey, *rsa.PublicKey, rsa.PrivateKey, *rsa.PrivateKey:
		kty = jwa.RSA
	case ecdsa.PublicKey, *ecdsa.PublicKey, ecdsa.PrivateKey, *ecdsa.PrivateKey:
		kty = jwa.EC
	case ed25519.PublicKey, ed25519.PrivateKey, x25519.PublicKey, x25519.PrivateKey:
		kty = jwa.OKP
	case []byte:
		kty = jwa.OctetSeq
	default:
		return nil, fmt.Errorf(`invalid key %T`, key)
	}

	algs, ok := keyTypeToAlgorithms[kty]
	if !ok {
		return nil, fmt.Errorf(`invalid key type %q`, kty)
	}
	return algs, nil
}

// Because the keys defined in github.com/lestrrat-go/jwx/jwk may also implement
// crypto.Signer, it would be possible for to mix up key types when signing/verifying
// for example, when we specify jws.WithKey(jwa.RSA256, cryptoSigner), the cryptoSigner
// can be for RSA, or any other type that implements crypto.Signer... even if it's for the
// wrong algorithm.
//
// These functions are there to differentiate between the valid KNOWN key types.
// For any other key type that is outside of the Go std library and our own code,
// we must rely on the user to be vigilant.
//
// Notes: symmetric keys are obviously not part of this. for v2 OKP keys,
// x25519 does not implement Sign()
func isValidRSAKey(key interface{}) bool {
	switch key.(type) {
	case
		ecdsa.PrivateKey, *ecdsa.PrivateKey,
		ed25519.PrivateKey,
		jwk.ECDSAPrivateKey, jwk.OKPPrivateKey:
		// these are NOT ok
		return false
	}
	return true
}

func isValidECDSAKey(key interface{}) bool {
	switch key.(type) {
	case
		ed25519.PrivateKey,
		rsa.PrivateKey, *rsa.PrivateKey,
		jwk.RSAPrivateKey, jwk.OKPPrivateKey:
		// these are NOT ok
		return false
	}
	return true
}

func isValidEDDSAKey(key interface{}) bool {
	switch key.(type) {
	case
		ecdsa.PrivateKey, *ecdsa.PrivateKey,
		rsa.PrivateKey, *rsa.PrivateKey,
		jwk.RSAPrivateKey, jwk.ECDSAPrivateKey:
		// these are NOT ok
		return false
	}
	return true
}
