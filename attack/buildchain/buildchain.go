package buildchain

import (
	"fmt"
	"math/big"
	"os"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/syndtr/goleveldb/leveldb"
	//"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/core/rawdb"
	ethdbLeveldb "github.com/ethereum/go-ethereum/ethdb/leveldb"
)

var PredictionChainDbPath string


func setChainDbPath() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	pathSeparator := string(os.PathSeparator)
	PredictionChainDbPath = home + pathSeparator + ".buildchain" + pathSeparator + "prediction_chain_db"
	return nil
}

func BuildChain(n int) error {
	if err := setChainDbPath(); err != nil {
		return err
	}

	db, err := leveldb.OpenFile(PredictionChainDbPath, nil)
	if err != nil {
		return err
	}
	defer db.Close()

	dbWrapper := ethdbLeveldb.NewSimple(db)
	batch := dbWrapper.NewBatch()

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

	headers := []*types.Header{genesisHeader}
	lastHeader := genesisHeader
	td := new(big.Int).Set(genesisHeader.Difficulty)

	for i := 1; i <= n; i++ {
		currHeader := &types.Header{
			ParentHash: lastHeader.Hash(),
			UncleHash: common.HexToHash("0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"),
			Coinbase: common.HexToAddress("0x0"),
			Root: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			TxHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			ReceiptHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			Bloom: types.BytesToBloom(common.FromHex("0x0")),
			Difficulty: big.NewInt(10),
			Number: big.NewInt(0).Add(lastHeader.Number, big.NewInt(1)),
			GasLimit: uint64(3141592),
			GasUsed: uint64(0),
			Time: lastHeader.Time + uint64(13),
			Extra: make([]byte, 0),
			MixDigest: common.HexToHash("0x0"),
			Nonce: types.EncodeNonce(uint64(0x2763ab980cd417ef)),
		}
		lastHeader = currHeader
		td.Add(td, currHeader.Difficulty)
		headers = append(headers, currHeader)

		rawdb.WriteTd(batch, currHeader.Hash(), currHeader.Number.Uint64(), td)
		rawdb.WriteHeader(batch, currHeader)
	}

	fmt.Println("Created chain of", n, "blocks and stored to disk")
	return nil
}

/*
func newBatch(db *leveldb.DB) ethdb.Batch {
	return &Batch{
		db: db,
		b:  new(leveldb.Batch),
	}
}
*/