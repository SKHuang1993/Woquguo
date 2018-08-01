package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	//	"time"
	//	"data"
	//	"Blog"
	//	"ydict"
	//	"webserve"
	"myannie"
)

func Add(a, b int) int {
	return a + b
}

//学习golang regexp 正则表达式的用法
func RegexpTest() {

	//FindAllString	Regexp的方法，匹配字符串，返回匹配结果组成一个 []string。限定参数 -1表示不限定，其它表示限定。

	//检查b中是否有符合匹配pattern的子序列
	fmt.Println(regexp.Match("H.* ", []byte("Hello World !")))
	fmt.Println(regexp.Match("D.* ", []byte("Hello World !")))

	//在s中查找re编译好的正则表达式，并返回第一个位子
	reg := regexp.MustCompile(`\w+`)

	fmt.Println(reg.FindStringIndex("Hello World"))

	// 在 b 中查找 re 中编译好的正则表达式，并返回所有匹配的位置
	// {{起始位置, 结束位置}, {起始位置, 结束位置}, ...}
	// 只查找前 n 个匹配项，如果 n < 0，则查找所有匹配项
	fmt.Println(reg.FindAllIndex([]byte("Hello World !"), -1))

}

func main() {

	//跑马航的接口，做数据搜索
	//Search()

	//ydict 字典查询
	//	ydict.LoadEnv()

	//	Blog.Blog()

	//	webserve.Start()

	//	RegexpTest()

	myannie.Init()

	return

}

func mainPost() {

	//baseUrl :="http://apiuat.iranmahanair.com/gsa-ws/mahan/search?"
	//version:="V1.2"
	//appId :="yiqifei"
	//privateKey := "0ff363f3-ebc0-4429-985e-05a5d990c830"
	////sessionId:=null
	//
	//depAirport:="PVG"
	//arrAirport:="DXB"
	//depDate:="20180226"
	//retDate:=":"
	//adultNum :="2"
	//childNum:="1"
	//preferCabin:="Y"

	body := bytes.NewBuffer([]byte(`{"adultNum":"1","appId":"yiqifei","arrAirport":"CDG","childNum":"0","depAirport":"CAN","depDate":"20180504","preferCabin":"Y","privateKey":"0ff363f3-ebc0-4429-985e-05a5d990c830","retDate":"20180510","version":"V1.2"}`))

	//fmt.Println(body)

	s := "http://apiuat.iranmahanair.com/gsa-ws/mahan/search"

	resp, err := http.Post(s, "application/json;charset=UTF-8", body)

	if err != nil {

		fmt.Println("错误了-----resp-")
		panic(err)
		return
	}

	defer resp.Body.Close()
	result, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		//fmt.Println("错误了-----result-")
		panic(err)
	}
	fmt.Println(string(result))

}

func chitChat() {
	mux := http.NewServeMux()

	//处理用户问题
	mux.HandleFunc("/login", login)
	mux.HandleFunc("/signup", signup)
	mux.HandleFunc("/signup_account", signup_account)
	mux.HandleFunc("/logout", logout)

	//处理帖子问题

	mux.HandleFunc("/thread/new", newThread)
	mux.HandleFunc("/thread/create", createThread)
	mux.HandleFunc("/thread/post", postThread)
	mux.HandleFunc("/thread/read", readThread)

	server := &http.Server{
		Addr:    config.Address,
		Handler: mux,
		//ReadTimeout:    config.ReadTimeout,
		//WriteTimeout:   config.ReadTimeout,
		//MaxHeaderBytes: 1 << 20,
	}

	server.ListenAndServe()
}
