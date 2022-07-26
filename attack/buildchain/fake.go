package buildchain

import (
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	originalHead *types.Block
)

func SetOriginalHead(head *types.Block) {
	originalHead = head
}