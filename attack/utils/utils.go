package utils

import "fmt"
import "os"
import "math/big"
import "errors"
import "github.com/ethereum/go-ethereum/core/types"
import "github.com/ethereum/go-ethereum/common"
import "github.com/ethereum/go-ethereum/core/rawdb"
import "github.com/ethereum/go-ethereum/params"

type AttackPhase byte

const (
	Debug = false

	//PredictionChainDbPath = "/home/massi/.buildchain/prediction_chain_db"

	StalePhase = AttackPhase(0)
	ReadyPhase = AttackPhase(1)
	PredictionPhase = AttackPhase(2)
	SyncPhase = AttackPhase(3)
	OtherPhase = AttackPhase(4)

	BatchSize = 192
	NumBatchesForPrediction = 8		// Corresponds to m+1, with m parameter in Section 13 of the Write-Up
									// We need m+1 to be even if the malicious peers are 2

	RequiredOracleBits = 14			// Corresponds to parameter n in Section 13 of the Write-Up.
									// Now set to 14 for testing purposes, otherwise >=31
	SeedSize = 4					// The seed is always 4 bytes long
)

var (
	BridgeClosedErr = errors.New("bridge closed")
	PartialSendErr  = errors.New("message sent only partially")
	PartialRecvErr  = errors.New("message received only partially")
	ParameterErr    = errors.New("parameter(s) is invalid")
	StateError      = errors.New("reached an invalid state")

	D0 = params.MinimumDifficulty							// In our simulation, the difficulty at the original head
															// when the attack starts is the same as the genesis
															// difficulty, which is the same as the minimum difficulty.
															// However, to run this attack in the real Ethereum network,
															// D0 should be set to the current difficulty (ca 14*10^15)
	
	HigherTd = new(big.Int).Mul(big.NewInt(1_000_000_000_000), big.NewInt(1_000_000_000_000)) // 10^24
	DifficultySupplement = new(big.Int).Mul(D0, big.NewInt(41))		// The value 41 is computed as 90% of the number of
																	// new blocks mined during the headers download.
																	// For a real attack, this would be ca. 500 (2hrs).

	RangeOne, _ = new(big.Int).SetString("0x1000000000000000000000000000000000000000", 0)
)

func (phase AttackPhase) ToChainType() ChainType {
	switch phase {
	case PredictionPhase:
		return PredictionChain
	case SyncPhase:
		return TrueChain
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
	case SyncPhase:
		return "sync"
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

func Export(dbPath, filename string) error {
	fh, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.ModePerm)
	if err != nil {
		fmt.Println("Could not open file " + filename)
		return err
	}

	db, err := rawdb.NewLevelDBDatabase(dbPath, 0, 0, "", false)
	if err != nil {
		fmt.Println("Could not load database at " + dbPath)
		return err
	}

	headHash := rawdb.ReadHeadHeaderHash(db)
	last := *(rawdb.ReadHeaderNumber(db, headHash))
	for nr := uint64(0); nr <= last; nr++ {
		var block *types.Block
		hash := rawdb.ReadCanonicalHash(db, nr)
		if hash == (common.Hash{}) {
			block = nil
		} else {
			block = rawdb.ReadBlock(db, hash, nr)
		}
		if block == nil {
			return fmt.Errorf("export failed on #%d: not found", nr)
		}
		if err := block.EncodeRLP(fh); err != nil {
			return err
		}
	}
	return nil
}