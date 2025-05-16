package apiHandler

import (
	"encoding/json"
	"github.com/Sabnaj-42/BookServer-API/authHandler"

	//"fmt"
	//"github.com/Sabnaj-42/BookServer-API/authHandler"
	dh "github.com/Sabnaj-42/BookServer-API/dataHandler"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"log"
	"net/http"
)

func getAllBooks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(dh.BookList)
	if err != nil {
		http.Error(w, "Cannot encode data", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)

}

func AddNewBook(w http.ResponseWriter, r *http.Request) {
	var book dh.Book
	err := json.NewDecoder(r.Body).Decode(&book)
	if err != nil {
		http.Error(w, "Cannot decode data", http.StatusBadRequest)
		return
	}
	if len(book.Name) == 0 || len(book.ISBN) == 0 || len(book.Authors) == 0 {
		http.Error(w, "Invalid Data Entry", http.StatusBadRequest)
		return
	}

	_, exists := dh.BookList[book.ISBN]
	if exists {
		http.Error(w, "Book already exists", http.StatusConflict)
		return
	}

	dh.BookList[book.ISBN] = book
	w.WriteHeader(http.StatusCreated)

}

func deleteBook(w http.ResponseWriter, r *http.Request) {
	var ISBN string
	ISBN = chi.URLParam(r, "ISBN")

	if len(ISBN) == 0 {
		http.Error(w, "Invalid ISBN", http.StatusBadRequest)
		return
	}
	_, exists := dh.BookList[ISBN]
	if !exists {
		http.Error(w, "Book does not exist", http.StatusNotFound)
		return
	}
	delete(dh.BookList, ISBN)
	w.WriteHeader(http.StatusOK)
}

func updateBook(w http.ResponseWriter, r *http.Request) {
	var ISBN string
	ISBN = chi.URLParam(r, "ISBN")
	if len(ISBN) == 0 {
		http.Error(w, "Invalid ISBN", http.StatusBadRequest)
		return
	}
	_, exists := dh.BookList[ISBN]
	if !exists {
		http.Error(w, "Book does not exist", http.StatusNotFound)
		return
	}

	var newBook dh.Book
	err := json.NewDecoder(r.Body).Decode(&newBook)
	if err != nil {
		http.Error(w, "Cannot decode data", http.StatusBadRequest)
		return
	}

	dh.BookList[ISBN] = newBook
	_, err = w.Write([]byte("Book updated successfully"))
	if err != nil {
		http.Error(w, "Can not write data", http.StatusInternalServerError)
	}
	w.WriteHeader(http.StatusOK)

}

func RunServer(port int) {

	dh.Init()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.URLFormat)

	//Protected
	r.Post("/signIn", authHandler.SignIn)
	r.Post("/login", authHandler.Login) // request for login:  curl -i  -X POST http://localhost:8080/login      -H "Content-Type: application/json"      -d '{"username": "sabnaj", "password": "1234"}'
	r.Post("/logout", authHandler.Logout)
	r.Post("/newBook", AddNewBook)
	r.Put("/updateBook", updateBook)
	r.Delete("/deleteBook", deleteBook)

	//unprotected
	r.Get("/getBooks", getAllBooks) //request for getBooks: curl http://localhost:8080/getBooks

	if err := http.ListenAndServe("127.0.0.1:8080", r); err != nil {
		log.Fatalln(err)
	}
}
