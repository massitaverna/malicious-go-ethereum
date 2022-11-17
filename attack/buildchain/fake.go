package buildchain

import (
	"fmt"
	"os"
	"bufio"
	"errors"
	mrand "math/rand"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/attack/utils"
)

const (
	reducedConstantMiningTime = 9
)

var (
	originalHead *types.Header
	ghostRoot common.Hash
	mgethDir string
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

func GenerateBuildParameters(Tm int, filename string, prng *mrand.Rand) (*BuildParameters, error) {
	file, err := os.Open(filename)
	if err !=  nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Scan()
	strategy := scanner.Text()
	bp := &BuildParameters{}
	offset := int(originalHead.Number.Uint64())

	switch(strategy) {
	case "none":
		return nil, errors.New("attack is infeasible")
	case "constant":
		n := Tm/reducedConstantMiningTime
		discard := (n - utils.MinFullyVerifiedBlocks) % utils.BatchSize
		if discard < 0 {
			discard += utils.BatchSize
		}
		n -= discard
		bp.NumBatches = n/utils.BatchSize
		bp.TimestampDeltasMap = make(map[int]int)
		for i := 1; i <= n; i++ {
			bp.TimestampDeltasMap[i+offset] = reducedConstantMiningTime
		}
	case "modulate":
		//TODO
	default:
		return nil, errors.New("unknown strategy: " + strategy)
	}


	// Generate seals
	bp.SealsMap = make(map[int]bool)
	for i := 1; i <= bp.NumBatches*utils.BatchSize + utils.MinFullyVerifiedBlocks; i++ {
		bp.SealsMap[offset+i] = false
	}
	fmt.Println("Blocks that will be verified (first 3 batches):")
	for i := 0; i < bp.NumBatches; i++ {
		s1 := prng.Intn(100)
		s2 := prng.Intn(100) + 100
		if s2 >= utils.BatchSize {
			s2 = utils.BatchSize - 1
		}
		bp.SealsMap[offset + i*utils.BatchSize + 1 + s1] = true
		bp.SealsMap[offset + i*utils.BatchSize + 1 + s2] = true
		bp.SealsMap[offset + (i+1)*utils.BatchSize] = true
		if i < 3 {
			fmt.Printf("%d, %d, %d\n", offset + i*utils.BatchSize + 1 + s1, offset + i*utils.BatchSize + 1 + s2, offset + (i+1)*utils.BatchSize)
		}
	}
	for i := 1; i <= utils.MinFullyVerifiedBlocks; i++ {
		bp.SealsMap[offset+bp.NumBatches*utils.BatchSize+i] = true
	}

	return bp, nil
}

func BuildParametersForTesting(prng *mrand.Rand) *BuildParameters {
	bp := &BuildParameters{
		NumBatches: 4,
		SealsMap: make(map[int]bool),
		TimestampDeltasMap: make(map[int]int),
	}

	offset := int(originalHead.Number.Uint64())
	amount := bp.NumBatches*utils.BatchSize+utils.MinFullyVerifiedBlocks
	for i := offset+1; i <= offset+amount; i++ {
		bp.SealsMap[i] = false
		bp.TimestampDeltasMap[i] = 13
	}

	fmt.Println("Blocks that will be verified (first 3 batches):")
	for i := 0; i < bp.NumBatches; i++ {
		s1 := prng.Intn(100)
		s2 := prng.Intn(100) + 100
		if s2 >= utils.BatchSize {
			s2 = utils.BatchSize - 1
		}
		bp.SealsMap[offset + i*utils.BatchSize + 1 + s1] = true
		bp.SealsMap[offset + i*utils.BatchSize + 1 + s2] = true
		bp.SealsMap[offset + (i+1)*utils.BatchSize] = true
		if i < 3 {
			fmt.Printf("%d, %d, %d\n", offset + i*utils.BatchSize + 1 + s1, offset + i*utils.BatchSize + 1 + s2, offset + (i+1)*utils.BatchSize)
		}
	}
	for i := 1; i <= utils.MinFullyVerifiedBlocks; i++ {
		bp.SealsMap[offset+bp.NumBatches*utils.BatchSize+i] = true
	}

	return bp
}
