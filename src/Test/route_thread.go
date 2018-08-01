package main

import (
	"fmt"
	"net/http"
)

func newThread(writer http.ResponseWriter, request *http.Request) {

	fmt.Fprint(writer, "newThread")
}

func createThread(writer http.ResponseWriter, request *http.Request) {

	fmt.Fprint(writer, "createThread")
}

func readThread(writer http.ResponseWriter, request *http.Request) {

	fmt.Fprint(writer, "readThread")
}

func postThread(writer http.ResponseWriter, request *http.Request) {

	fmt.Fprint(writer, "postThread")
}
