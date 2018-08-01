package data

import (
	"net/http"
	"time"
)

type Thread struct {
	Id        int
	Uuid      string
	Topic     string
	User_id   int
	CreatedAt time.Time
}

type Post struct {
	Id        int
	Uuid      string
	Body      string
	Topic     string
	UserId    int
	ThreadId  int
	CreatedAt time.Time
}

func newThread(writer http.ResponseWriter, request *http.Request) {

}

func createThread(writer http.ResponseWriter, request *http.Request) {

}

func postThread(writer http.ResponseWriter, request *http.Request) {

}

func readThread(writer http.ResponseWriter, request *http.Request) {

}
