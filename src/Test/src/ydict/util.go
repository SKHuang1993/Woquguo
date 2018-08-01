/**
1.path/filepath 这个包需要了解


*/

package ydict

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	//	"strings"
	"unicode"

	"github.com/fatih/color"
)

var (
	Version = "0.1"
	logo    = `
██╗   ██╗██████╗ ██╗ ██████╗████████╗
╚██╗ ██╔╝██╔══██╗██║██╔════╝╚══██╔══╝
 ╚████╔╝ ██║  ██║██║██║        ██║   
  ╚██╔╝  ██║  ██║██║██║        ██║   
   ██║   ██████╔╝██║╚██████╗   ██║   
   ╚═╝   ╚═════╝ ╚═╝ ╚═════╝   ╚═╝   
YDict V%s
https://github.com/TimothyYe/ydict
`
)

func displayUsage() {

	color.Cyan(logo, Version)
	color.Red("Usage:")
	color.Yellow("ydict <word(s) to query>        Query the word(s)")
	color.Blue("ydict <word(s) to query> -v     Query with speech")
	color.Green("ydict <word(s) to query> -v     Query with speech")

}

func isAvailableOS() bool {

	return runtime.GOOS == "darwin" || runtime.GOOS == "linux"
}

//是否中文
func isChinese(str string) bool {

	for _, c := range str {

		if unicode.Is(unicode.Scripts["Han"], c) {
			return true
		}

	}

	return false
}

func getExecutePath() string {

	ex, err := os.Executable()
	fmt.Println(ex)
	if err != nil {
		panic(err)
	}
	return filepath.Dir(ex)

}

/*
//解析参数，返回解析后切出来的字符串，是否播放语音，是否搜索更多
func parseArgs(args []string) ([]string, bool, bool) {

	var withVoice, withMore bool
	parameterStartIndex := findParamStartIndex(args)


}



func findParamStartIndex(args []string) int {

	for index, word := range args {
		if strings.HasPrefix(word, "-") && len(word) == 2 {
			return index

		}
		return len(args)
	}

}
*/
//在字符串数组中寻找指定的字符串
func elementInStringArray(stringArray []string, str string) bool {
	for _, word := range stringArray {
		if word == str {
			return true
		}
	}
	return false

}
