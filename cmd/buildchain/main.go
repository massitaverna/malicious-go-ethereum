package main

import (
	"fmt"
	"flag"
	"github.com/ethereum/go-ethereum/attack/buildchain"
	"github.com/ethereum/go-ethereum/attack/utils"
)

func main() {
	fmt.Println("Started buildchain tool")

	numBlocks := flag.Int("n", 0, "Number of blocks")
	overwrite := flag.Bool("overwrite", false, "Build a new chain even if an equivalent one already exists, and overwrite it")
	flag.Parse()

	err := buildchain.BuildChain(utils.PredictionChain, *numBlocks, *overwrite)
	if err != nil {
		fmt.Println("Could not build the chain")
		fmt.Println("err =", err)
	}
}
