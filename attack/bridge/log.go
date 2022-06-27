package bridge

import "fmt"
import "time"

const (
	colorReset = "\033[0m"
	colorRedBg = "\033[37;41m"
)

//var timeout = time.NewTimer(100*time.Millisecond)

func log(a ...interface{}) {
	timestamp := time.Now().Format("[01-02|15:04:05.000]")
	//<-timeout.C
	//timeout.Reset(100*time.Millisecond)
	fmt.Print(colorRedBg + "ATCK" + colorReset + " " + timestamp + " ")
	fmt.Println(a...)
}
