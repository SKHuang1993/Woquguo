package webserve

/*
import (
	"fmt"

	"database/sql"

	_ "github.com/go-sql-driver/mysql"
)


var DB *sql.DB

func init() {
	DB, err := sql.Open("mysql", "root:@tcp(127.0.0.1:3306)/DB1?parseTime=true")
	if err != nil {
		panic(err)
	}
	defer DB.Close()
}

func retrieve(id int) (post Post, err error) {

	post = Post{}
	err = DB.QueryRow()

}

func (post *Post) create() (err error) {

	stmt, err := DB.Prepare("insert into posts(content,author) values(?,?)")
	if err != nil {
		panic(err)
	}
	defer stmt.Close()

	result, err := stmt.Exec(post.Content, post.Author)
	if err != nil {
		panic(err)
	}
	return

}

func (post *Post) update() (err error) {

	stmt, err := DB.Prepare("update posts set content=?,author = ? where id =?")
	if err != nil {
		panic(err)
	}
	result, err := stmt.Exec(post.Content, post.Author, post.Id)
	if err != nil {
		panic(err)
	}
	return

}

func (post *Post) delete() (err error) {

	stmt, err := DB.Prepare("delete posts where id =?")
	if err != nil {
		panic(err)
	}
	result, err := stmt.Exec(post.Id)
	if err != nil {
		panic(err)
	}
	return

}
*/
