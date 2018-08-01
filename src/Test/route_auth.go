package main

import (
	//	"data"
	"fmt"
	"html/template"
	"net/http"
)

//Get Loginpage
func login(writer http.ResponseWriter, request *http.Request) {

	fmt.Println("调用了login...")

}

//Get show the signup页面

func signup(writer http.ResponseWriter, request *http.Request) {

	fmt.Fprint(writer, "加载注册页面")
	t, _ := template.ParseFiles("./public/signup.html")
	t.Execute(writer, nil)

}

func signup_account(writer http.ResponseWriter, request *http.Request) {
	fmt.Fprintf(writer, "调取注册资料的接口")
}

//Get logout
//退出用户

func logout(writer http.ResponseWriter, request *http.Request) {

}
