package bridge

import "github.com/ethereum/go-ethereum/common"

type HashOrNumber struct {
	Hash common.Hash
	Number uint64
}

type GetBlockHeadersPacket struct {
	Origin HashOrNumber
	Amount uint64
	Skip uint64
	Reverse bool
}

/*
type prngSteps struct {
	frequency int
	num int
}
*/