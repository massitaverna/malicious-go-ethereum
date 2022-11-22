package utils

import "fmt"

type ChainType byte

const (
	InvalidChainType = ChainType(0)
	PredictionChain = ChainType(1)
	TrueChain = ChainType(2)
	FakeChain = ChainType(3)
)

func (ct ChainType) GetDir() string {
	var dir string

	switch (ct) {
	case PredictionChain:
		dir = "prediction_chain_db"
	case TrueChain:
		dir = "true2_chain_db"
	case FakeChain:
		dir = "fake_chain_segment_db"
	default:
		panic(fmt.Errorf("Invalid chain type: %d", byte(ct)))
	}
	
	return dir
}

func (ct ChainType) String() string {
	var s string

	switch (ct) {
	case PredictionChain:
		s = "prediction"
	case TrueChain:
		s = "true"
	case FakeChain:
		s = "fake"
	default:
		panic(fmt.Errorf("Invalid chain type: %d", byte(ct)))
	}

	return s
}

func StringToChainType(s string) (ChainType, error) {
	switch s {
	case "prediction":
		return PredictionChain, nil
	case "true":
		return TrueChain, nil
	case "fake":
		return FakeChain, nil
	default:
		return InvalidChainType, ParameterErr
	}
}

func AllChainsNames() string {
	return "'prediction', 'true', 'fake'"
}
