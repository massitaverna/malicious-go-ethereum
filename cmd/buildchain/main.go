package main

import (
	"fmt"
	"github.com/ethereum/go-ethereum/attack/buildchain"
)

func main() {
	fmt.Println("Started buildchain tool")
	err := buildchain.BuildChain(7*192)
	if err != nil {
		fmt.Println("Could not build the chain")
		fmt.Println("err =", err)
	}
}
