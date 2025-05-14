package main

import (
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"io/ioutil"
	"log"
	"strings"

	//"github.com/go-chi/chi/v5"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"net/http"
	"time"
)

// Secret is the secret key used for java web token verify signature
var Secret = []byte("this_is_my_secret_key")

type Author struct { //Hold common information of an Aurhor
	Name string `json:"name"`
	Home string `json:"home"`
}

/*type AuthorBooks struct { //Hold information of author and corresponding book
	Author `json:"author"`
	Books  []string `json:"books"`
}*/

type Book struct { // Information about book
	Name    string   `json:"name"`
	Authors []Author `json:"authors"`
	ISBN    string   `json:"isbn"`
	Genre   string   `json:"genre"`
	Pub     string   `json:"pub"`
}

type Credentials struct { //Login credentials
	Username string `json:"username"`
	Password string `json:"password"`
}

type AuthorDB map[string]Author
type BookDB map[string]Book
type CredentialDB map[string]string

var bookList BookDB
var credentialList CredentialDB
var authorList AuthorDB

func Init() { //initializing data for book server

	credentialList = make(CredentialDB)
	bookList = make(BookDB)

	credentialList["sabnaj"] = "1234"
	credentialList["Admin"] = "5678"

	author1 := Author{
		Name: "Sadia Sornaly",
		Home: "Korea",
	}
	author2 := Author{
		Name: "Shahana",
		Home: "America",
	}

	book1 := Book{
		Name:    "Book 1",
		Authors: []Author{author1, author2},
		ISBN:    "ISBN 1",
		Genre:   "Thriller",
		Pub:     "Unknown",
	}
	book2 := Book{
		Name:    "Book 2",
		Authors: []Author{author1},
		ISBN:    "ISBN 2",
		Genre:   "Science Fiction",
		Pub:     "Tor Books",
	}
	//authorList[author1.Name] = author1
	//authorList[author2.Name] = author2

	bookList[book1.ISBN] = book1
	bookList[book2.ISBN] = book2

}

func SmStr(str string) string { //convert string into small letter
	return strings.ToLower(str)
}

// function for login
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

func getAllBooks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(bookList)
	if err != nil {
		http.Error(w, "Cannot encode data", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)

}

func AddNewBook(w http.ResponseWriter, r *http.Request) {
	var book Book
	err := json.NewDecoder(r.Body).Decode(&book)
	if err != nil {
		http.Error(w, "Cannot decode data", http.StatusBadRequest)
		return
	}
	if len(book.Name) == 0 || len(book.ISBN) == 0 || len(book.Authors) == 0 {
		http.Error(w, "Invalid Data Entry", http.StatusBadRequest)
		return
	}

	_, exists := bookList[book.ISBN]
	if exists {
		http.Error(w, "Book already exists", http.StatusConflict)
		return
	}

	bookList[book.ISBN] = book
	w.WriteHeader(http.StatusCreated)

}

func deleteBook(w http.ResponseWriter, r *http.Request) {
	var ISBN string
	ISBN = chi.URLParam(r, "ISBN")

	if len(ISBN) == 0 {
		http.Error(w, "Invalid ISBN", http.StatusBadRequest)
		return
	}
	_, exists := bookList[ISBN]
	if !exists {
		http.Error(w, "Book does not exist", http.StatusNotFound)
		return
	}
	delete(bookList, ISBN)
	w.WriteHeader(http.StatusOK)
}

func updateBook(w http.ResponseWriter, r *http.Request) {
	var ISBN string
	ISBN = chi.URLParam(r, "ISBN")
	if len(ISBN) == 0 {
		http.Error(w, "Invalid ISBN", http.StatusBadRequest)
		return
	}
	_, exists := bookList[ISBN]
	if !exists {
		http.Error(w, "Book does not exist", http.StatusNotFound)
		return
	}

	var newBook Book
	err := json.NewDecoder(r.Body).Decode(&newBook)
	if err != nil {
		http.Error(w, "Cannot decode data", http.StatusBadRequest)
		return
	}

	bookList[ISBN] = newBook
	_, err = w.Write([]byte("Book updated successfully"))
	if err != nil {
		http.Error(w, "Can not write data", http.StatusInternalServerError)
	}
	w.WriteHeader(http.StatusOK)

}

func main() {
	Init()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.URLFormat)

	//Protected
	r.Post("/signIn", SignIn)
	r.Post("/login", Login) // request for login:  curl -i  -X POST http://localhost:8080/login      -H "Content-Type: application/json"      -d '{"username": "sabnaj", "password": "1234"}'
	r.Post("/logout", Logout)
	r.Post("/newBook", AddNewBook)
	r.Put("/updateBook", updateBook)
	r.Delete("/deleteBook", deleteBook)

	//unprotected
	r.Get("/getBooks", getAllBooks) //request for getBooks: curl http://localhost:8080/getBooks

	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalln(err)
	}

}
