package apiHandler

import (
	"github.com/go-chi/chi/v5"
	"net/http"
)

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
