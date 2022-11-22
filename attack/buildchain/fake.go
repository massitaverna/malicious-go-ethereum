package buildchain

import (
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/common"
)

const (
	reducedConstantMiningTime = 9
)

var (
	originalHead *types.Header
	ghostRoot common.Hash
	mgethDir string
	hashrate int64
)

func SetOriginalHead(head *types.Header) {
	originalHead = head
}

func SetGhostRoot(root []byte) {
	ghostRoot = common.BytesToHash(root)
}

func GhostRootSet() bool {
	return ghostRoot!=common.Hash{}
}

func SetHashrateLimit(limit int64) {
	hashrate = limit
}

// TODO: Rename to SetPeerCwd()
func SetMgethDir(path string) {
	mgethDir = path
}

func SetSeals(m map[int]bool) {
	sealsMap = m
}
func SetTimestampDeltas(m map[int]int) {
	timestampDeltasMap = m
}


type BuildParameters struct {
	NumBatches int
	SealsMap map[int]bool
	TimestampDeltasMap map[int]int
}

func GenerateBuildParameters() (*BuildParameters, error) {
	bp := &BuildParameters{
		NumBatches: 1,
		SealsMap: make(map[int]bool),
		TimestampDeltasMap: make(map[int]int),
	}
	offset := int(originalHead.Number.Uint64())


	// Set seals
	bp.SealsMap = make(map[int]bool)
	for i := 1; i <= 128; i++ {
		bp.SealsMap[offset+i] = true
	}

	// Set timestamp distances
	for i := 1; i <= 128; i++ {
		if i <= 40 {
			bp.TimestampDeltasMap[offset+i] = 900
		} else {
			bp.TimestampDeltasMap[offset+i] = 9
		}
	}

	return bp, nil
}
