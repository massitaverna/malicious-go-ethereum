package main

import "fmt"
import "github.com/ethereum/go-ethereum/attack/orchestrator"

func main() {
	errc := make(chan error)				// Channel to signal the first error or success

	orch := orchestrator.New(errc)
	orch.Start()

	err := <-errc							// Wait for something to happen
	if err != nil {
		fmt.Println("Attack failed")
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Attack succeeded")
	}
	

	//orch.Wait()
}
