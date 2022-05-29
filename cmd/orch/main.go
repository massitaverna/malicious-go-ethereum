package main

import "fmt"
import "flag"
import "github.com/ethereum/go-ethereum/attack/orchestrator"

func main() {
	rebuild := flag.Bool("rebuild", false, "Rebuild underlying chain(s) and overwrite them in buildchain tool's directory")
	flag.Parse()
	errc := make(chan error)				// Channel to signal the first error or success

	orch := orchestrator.New(errc)
	orch.Start(*rebuild)
	orch.Wait()

	err := <-errc							// Wait for something to happen
	if err != nil {
		fmt.Println("Attack failed")
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Attack succeeded")
	}
}
