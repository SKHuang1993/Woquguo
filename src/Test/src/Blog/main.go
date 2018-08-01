package Blog

import (
	"fmt"

	"database/sql"
	"net/http"

	"log"

	_ "github.com/go-sql-driver/mysql"
	"gopkg.in/gin-gonic/gin.v1"
)

func Blog() {
	fmt.Println("this is Blog package")

	db, err := sql.Open("mysql", "root:@tcp(127.0.0.1:3306)/DB1?parseTime=true")

	defer db.Close()
	if err != nil {
		log.Fatalln(err)
	}

	db.SetMaxIdleConns(20)
	db.SetMaxOpenConns(20)
	if err := db.Ping(); err != nil {
		log.Fatalln(err)
	}

	router := gin.Default()

	router.GET("/", func(c *gin.Context) {

		c.String(http.StatusOK, "Hello World")

	})

	router.POST("/person", func(c *gin.Context) {
		firstName := c.Request.FormValue("first_name")
		lastName := c.Request.FormValue("last_name")

		rs, err := db.Exec("insert into person(first_name, last_name) values(?,?)", firstName, lastName)
		if err != nil {
			log.Fatalln(err)
		}
		id, err := rs.LastInsertId()

		msg := fmt.Sprintf("insert successful %d", id)
		c.JSON(http.StatusOK, gin.H{

			"msg": msg,
		})

	})

	router.Run(":8080")

}
