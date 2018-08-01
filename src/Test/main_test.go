package main

import (
	"testing"
)

/*
//单元测试
func TestAdd(t *testing.T) {

	sum := Add(1, 2)
	if sum == 3 {
		t.Log("this result is ok")
	} else {
		t.Fatal("the result is wrong")
	}


}

*/

//表组测试
func TestAdd(t *testing.T) {
	sum := Add(1, 2)
	if sum == 3 {
		t.Log("this result is ok")
	} else {
		t.Fatal("the result is wrong")
	}

	sum = Add(3, 4)
	if sum == 7 {
		t.Log("this result is ok")
	} else {
		t.Fatal("the result is wrong")
	}

}
