package main

import (
	"encoding/json"
	//	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Configuration struct {
	Address      string
	ReadTimeout  int64
	WriteTimeout int64
	Static       string
}

var config Configuration
var logger *log.Logger

//打印
func p(a ...interface{}) {
	fmt.Print(a)
}

func init() {

	loadConfig()
	file, err := os.OpenFile("chitchat.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalln("Failed to open log file", err)
	}
	logger = log.New(file, "INFO", log.Ldate|log.Ltime|log.Lshortfile)

}

func loadConfig() {

	file, err := os.Open("config.json")
	if err != nil {
		log.Fatalln("Cannot open config file")
	}

	decoder := json.NewDecoder(file)
	config = Configuration{}
	err = decoder.Decode(&config)
	if err != nil {
		log.Fatalln("Cannot get configuration from file ", err)
	}
}

func error_message(write http.ResponseWriter, request *http.Request, msg string) {

	url := []string{"/err?msg=", msg}
	http.Redirect(write, request, strings.Join(url, ""), 302)

}

/*
func session(write http.ResponseWriter, request *http.Request) (sess data.Session, err error) {

	cookie, err := request.Cookie("_cookie")
	if err != nil {
		sess = data.Session{Uuid: cookie.Value}
		if ok, _ := sess.Check(); !ok {
			err = errors.New("Invalid session")
		}

	}
}
*/

func parseTemplateFile(filenames ...string) (t *template.Template) {

	var files []string
	t = template.New("layout")
	for _, file := range filenames {
		files = append(files, fmt.Sprintf("templates/%s.html", file))
	}

	t = template.Must(t.ParseFiles(files...))
	return
}

func generateHTML(writer http.ResponseWriter, data interface{}, filenames ...string) {

	var files []string
	for _, file := range filenames {
		files = append(files, fmt.Sprintf("templates/%s.html", file))
	}

	templates := template.Must(template.ParseFiles(files...))
	templates.ExecuteTemplate(writer, "layout", data)

}

//for logging
func info(args ...interface{}) {
	logger.SetPrefix("INFO")
	logger.Println(args...)
}

func danger(args ...interface{}) {
	logger.SetPrefix("DANGER")
	logger.Println(args...)

}

func error(args ...interface{}) {
	logger.SetPrefix("ERROR")
	logger.Println(args...)
}

func version() string {
	return "0.1"
}

/**
接下来的函数是给KS系统调用的
调用一些常用的函数

*/

func changeTime(t string) (currentTime string) {

	temp, _ := time.Parse("2006-01-02 15:04", t)
	fmt.Println(temp)
	return "323"

}
