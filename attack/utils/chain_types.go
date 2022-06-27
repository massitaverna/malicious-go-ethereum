package utils

type ChainType byte

const (
	InvalidChainType = ChainType(0)
	PredictionChain = ChainType(1)
	OtherChain = ChainType(2)
)

func (ct ChainType) GetDir() string {
	if ct == PredictionChain {
		return "prediction_chain_db"
	} else {
		return "other_chain_db"
	}
}

func (ct ChainType) String() string {
	var s string

	switch (ct) {
	case PredictionChain:
		s = "prediction"
	default:
		s = "other"
	}

	return s
}

func StringToChainType(s string) (ChainType, error) {
	switch s {
	case "prediction":
		return PredictionChain, nil
	case "other":
		return OtherChain, nil
	default:
		return InvalidChainType, ParameterErr
	}
}

func AllChainsNames() string {
	return "'prediction', 'other'"
}
