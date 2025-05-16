package authHandler

import (
	"fmt"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"io/ioutil"
	"net/http"
	"time"
)

var Secret = []byte("this_is_my_secret_key")

func Login(w http.ResponseWriter, r *http.Request) {
	var cred Credentials

	err := json.NewDecoder(r.Body).Decode(&cred)

	if err != nil {
		http.Error(w, "Cannot decode data", http.StatusBadRequest)
		return
	}

	password, ok := credentialList[cred.Username]
	if !ok {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	if password != cred.Password {
		http.Error(w, "Wrong password", http.StatusNotFound)
		return
	}

	//JWT token generation
	et := time.Now().Add(20 * time.Minute)
	token, err := jwt.NewBuilder().Audience([]string{"sabnaj"}).Expiration(et).Build()
	if err != nil {
		http.Error(w, "Cannot create token", http.StatusInternalServerError)
		return
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.HS256, Secret))
	if err != nil {
		http.Error(w, "Cannot sign token", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:    "jwt",
		Value:   string(signed),
		Expires: et,
	})
	w.Write([]byte("Login successful"))

	w.WriteHeader(http.StatusOK)
}

func Logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:    "jwt",
		Expires: time.Now(),
	})
	w.WriteHeader(http.StatusOK)
}

// function for signin
func SignIn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Read the request body
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Unable to read request body", http.StatusBadRequest)
		return
	}

	var user Credentials
	// Unmarshal JSON into the User struct
	err = json.Unmarshal(body, &user)
	if err != nil {
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	// Check if user already exists
	if _, exists := credentialList[user.Username]; exists {
		http.Error(w, "User already exists", http.StatusConflict)
		return
	}

	// Add user to the map
	credentialList[user.Username] = user.Password
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "User %s registered successfully", user.Username)
}
