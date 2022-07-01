package utils

type ChainType byte

const (
	InvalidChainType = ChainType(0)
	PredictionChain = ChainType(1)
	TrueChain = ChainType(2)
	OtherChain = ChainType(3)
)

func (ct ChainType) GetDir() string {
	var dir string

	switch(ct) {
	case PredictionChain:
		dir = "prediction_chain_db"
	case TrueChain:
		dir = "true_chain_db"
	default:
		dir = "other_chain_db"
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
	default:
		s = "other"
	}

	return s
}

func StringToChainType(s string) (ChainType, error) {
	switch s {
	case "prediction":
		return PredictionChain, nil
	case "true":
		return TrueChain, nil
	case "other":
		return OtherChain, nil
	default:
		return InvalidChainType, ParameterErr
	}
}

func AllChainsNames() string {
	return "'prediction', 'true', 'other'"
}
