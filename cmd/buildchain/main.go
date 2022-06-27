package main

import (
	"fmt"
	"os"
	"time"
	"flag"
	"math/rand"
	"github.com/ethereum/go-ethereum/attack/buildchain"
	"github.com/ethereum/go-ethereum/attack/utils"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	fmt.Println("Started buildchain tool")

	numBlocks := flag.Int("n", 0, "Number of blocks")
	overwrite := flag.Bool("overwrite", false, "Build a new chain even if an equivalent one already exists, and overwrite it")
	typeName  := flag.String("type", "prediction", "Type of chain to build (allowed: " + utils.AllChainsNames() + ")")
	export    := flag.String("export", "not set", "If set, export the chain to the specified file in RLP-encoded form. Useful for imports with 'geth import'")
	flag.Parse()
	chaintype, err := utils.StringToChainType(*typeName)
	if err != nil {
		fmt.Println("Invalid chain type: " + *typeName)
		return
	}

	if !isFlagPassed("type") {
		fmt.Println("Specify chain type with flag --type")
		return
	}
	if isFlagPassed("n") {
		err = buildchain.BuildChain(chaintype, *numBlocks, *overwrite)
		if err != nil {
			fmt.Println("Could not build the chain")
			fmt.Println("err =", err)
		}
	}

	if isFlagPassed("export") {
		err = buildchain.Export(chaintype, *export)
		if err != nil {
			fmt.Println("Could not export the chain")
			fmt.Println("err =", err)
		}
	}

	if !isFlagPassed("n") && !isFlagPassed("export") {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
}
	})
	return found
}
