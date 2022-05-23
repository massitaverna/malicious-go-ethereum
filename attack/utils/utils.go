package utils

import "math/big"
import "errors"

type AttackPhase byte

const (
	Debug = false

	PredictionChainDbPath = "/home/massi/.buildchain/prediction_chain_db"


	PredictionPhase = 1
	OtherPhase = 2

)

var (
	BridgeClosedErr = errors.New("bridge closed")
	PartialSendErr  = errors.New("message sent only partially")

	HigherTd = big.NewInt(100_000_000_000_000_000)
)