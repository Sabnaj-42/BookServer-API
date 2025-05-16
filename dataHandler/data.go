package dataHandler

import "strings"

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

var BookList BookDB
var CredentialList CredentialDB
var authorList AuthorDB

func Init() { //initializing data for book server

	CredentialList = make(CredentialDB)
	BookList = make(BookDB)

	CredentialList["sabnaj"] = "1234"
	CredentialList["Admin"] = "5678"

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

	BookList[book1.ISBN] = book1
	BookList[book2.ISBN] = book2

}

func SmStr(str string) string { //convert string into small letter
	return strings.ToLower(str)
}
