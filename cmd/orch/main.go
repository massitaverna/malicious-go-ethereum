package main

import "github.com/ethereum/go-ethereum/attack/orchestrator"

func main() {
	orch := orchestrator.New()
	orch.Start()
	orch.Wait()
}
