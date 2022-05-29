package utils

import "math/big"
import "errors"
//import "github.com/ethereum/go-ethereum/core/types"

type AttackPhase byte

const (
	Debug = false

	//PredictionChainDbPath = "/home/massi/.buildchain/prediction_chain_db"

	StalePhase = AttackPhase(0)
	ReadyPhase = AttackPhase(1)
	PredictionPhase = AttackPhase(2)
	OtherPhase = AttackPhase(3)

	BatchSize = 192
	NumBatchesForPrediction = 8		// Corresponds to m+1, with m parameter in Section 13 of the Write-Up
									// We need m+1 to be even if the malicious peers are 2

	RequiredOracleBits = 31			// Corresponds to parameter n in Section 13 of the Write-Up

)

var (
	BridgeClosedErr = errors.New("bridge closed")
	PartialSendErr  = errors.New("message sent only partially")
	ParameterErr    = errors.New("parameter(s) is invalid")

	HigherTd = big.NewInt(100_000_000_000_000_000)
)

func (phase AttackPhase) ToChainType() ChainType {
	switch phase {
	case PredictionPhase:
		return PredictionChain
	case OtherPhase:
		return OtherChain
	default:
		return InvalidChainType
	}
}

func (phase AttackPhase) String() string {
	switch phase {
	case StalePhase:
		return "stale"
	case ReadyPhase:
		return "ready"
	case PredictionPhase:
		return "prediction"
	default:
		return "other"
	}
}

/*
func GetHigherHead() *types.Header {
	return &types.Header{
		ParentHash: common.HexToHash("0x0"),
		UncleHash: common.HexToHash("0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"),
		Coinbase: common.HexToAddress("0x0"),
		Root: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		TxHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		ReceiptHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		Bloom: types.BytesToBloom(common.FromHex("0x0")),
		Difficulty: params.MinimumDifficulty,
		Number: big.NewInt(100_000_000),
		GasLimit: uint64(3141592),
		GasUsed: uint64(0),
		Time: uint64(time.Now().Unix() - 1),			// Make it as recent as possible
		Extra: make([]byte, 0),
		MixDigest: common.HexToHash("0x0"),
		Nonce: types.EncodeNonce(uint64(0)),
	}
}

func GetHigherPivot() *types.Header {
	higherHead = GetHigherHead()
	return &types.Header{
		ParentHash: common.HexToHash("0x0"),
		UncleHash: common.HexToHash("0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"),
		Coinbase: common.HexToAddress("0x0"),
		Root: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		TxHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		ReceiptHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		Bloom: types.BytesToBloom(common.FromHex("0x0")),
		Difficulty: params.MinimumDifficulty,
		Number: big.NewInt(higherHead.Number.Uint64() - 64),
		GasLimit: uint64(3141592),
		GasUsed: uint64(0),
		Time: uint64(higherHead.Time - 13*64),
		Extra: make([]byte, 0),
		MixDigest: common.HexToHash("0x0"),
		Nonce: types.EncodeNonce(uint64(0)),
	}
}
*/