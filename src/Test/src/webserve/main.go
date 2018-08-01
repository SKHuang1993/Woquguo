package webserve

//"fmt"
//"net/http"

type Post struct {
	Id      int    `json:"id"`
	Content string `json:"content"`
	Author  string `json:"author"`
}

/*
func hanleRequest(w http.ResponseWriter, r *http.Request) {

	var err error
	switch r.Method {

	case "GET":
		err = handleGET(w, r)
	case "POST":
		err = handlePOST(w, r)
	case "PUT":
		err = handlePUT(w, r)
	case "DELETE":
		err = handlePUT(w, r)

	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

}


func handleGET(w http.ResponseWriter, r *http.Request) (err error) {

}

func handlePOST(w http.ResponseWriter, r *http.Request) (err error) {

}
func handlePUT(w http.ResponseWriter, r *http.Request) (err error) {

}
func handleDELETE(w http.ResponseWriter, r *http.Request) (err error) {

}

func post(w http.ResponseWriter, r *http.Request) {

	//	fmt.Sprintf(w, "post")

}

func Start() {
	fmt.Println("第7章，创建Go Web服务")

	server := http.Server{
		Addr: "127.0.0.1:8080",
	}

	http.HandleFunc("/post", post)
	server.ListenAndServe()
}
*/
