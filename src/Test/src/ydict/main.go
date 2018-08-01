package ydict

import (
	"fmt"
)

func LoadEnv() {

	fmt.Println("LoadEnv")
	displayUsage()

	var c bool
	//	c = isChinese("wrw一起飞")
	c = isAvailableOS()

	fmt.Println(c)

	var s string

	s = getExecutePath()
	fmt.Println(s)
}
