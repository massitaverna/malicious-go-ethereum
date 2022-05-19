package bridge

import "fmt"
import "time"

const (
	colorReset  = "\033[0m"
	colorPurple = "\033[37;41m"
)

func log(a ...interface{}) {
	timestamp := time.Now().Format("[01-02|15:04:05.000]")
	fmt.Print(colorPurple + "ATCK" + colorReset + " " + timestamp + " ")
	fmt.Println(a...)
}
