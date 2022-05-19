package main

import (
	"fmt"
	"math/big"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/common"
)

func main() {
	fmt.Println("This is buildchain tool")
	buildChain()
}

func buildChain() {
	genesisHeader := &types.Header{
		ParentHash: common.HexToHash("0x0"),
		UncleHash: common.HexToHash("0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"),
		Coinbase: common.HexToAddress("0x0"),
		Root: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		TxHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		ReceiptHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		Bloom: types.BytesToBloom(common.FromHex("0x0")),
		Difficulty: big.NewInt(10),
		Number: big.NewInt(0),
		GasLimit: uint64(3141592),
		GasUsed: uint64(0),
		Time: uint64(0),
		Extra: make([]byte, 0),
		MixDigest: common.HexToHash("0x0"),
		Nonce: types.EncodeNonce(uint64(0x2763ab980cd417ef)),
	}

	fmt.Println("Created block #", genesisHeader.Number.Uint64())
}
