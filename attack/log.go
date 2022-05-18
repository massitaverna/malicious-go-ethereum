package attack

import "fmt"

const (
	colorReset  = "\033[0m"
	colorPurple = "\033[35m"
)

func Log(a ...interface{}) (n int, err error) {
	return fmt.Println(colorPurple, "ATCK ", colorReset, a)
}
