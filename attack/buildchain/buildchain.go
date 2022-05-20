package buildchain

import (
	"fmt"
	"math/big"
	"os"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/ethereum/go-ethereum/core/rawdb"
	ethdbLeveldb "github.com/ethereum/go-ethereum/ethdb/leveldb"
	"github.com/ethereum/go-ethereum/attack/utils"
)

const minimumDifficulty = 131072
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

	// Enable geth logs if debugging
	if utils.Debug {
		log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StdoutHandler))
	}

	// Create consensus engine
	ethashConfig := ethash.Config{
		CacheDir:         "ethash",
		CachesInMem:      2,
		CachesOnDisk:     3,
		CachesLockMmap:   false,
		DatasetsInMem:    1,
		DatasetsOnDisk:   2,
		DatasetsLockMmap: false,
	}

	engine := ethash.New(ethashConfig, nil, true)

	// Create/open database
	if err := setChainDbPath(); err != nil {
		return err
	}

	db, err := leveldb.OpenFile(PredictionChainDbPath, nil)
	if err != nil {
		return err
	}
	defer db.Close()

	dbWrapper := ethdbLeveldb.NewSimple(db)
	batch := dbWrapper.NewBatch()				// Batch object to write to database


	// Header on top of which mining starts
	genesisHeader := &types.Header{
		ParentHash: common.HexToHash("0x0"),
		UncleHash: common.HexToHash("0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"),
		Coinbase: common.HexToAddress("0x0"),
		Root: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		TxHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		ReceiptHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
		Bloom: types.BytesToBloom(common.FromHex("0x0")),
		Difficulty: big.NewInt(minimumDifficulty),
		Number: big.NewInt(0),
		GasLimit: uint64(3141592),
		GasUsed: uint64(0),
		Time: uint64(0),
		Extra: make([]byte, 0),
		MixDigest: common.HexToHash("0x0"),
		Nonce: types.EncodeNonce(uint64(0x2763ab980cd417ef)),
	}

	//headers := []*types.Header{genesisHeader}
	lastHeader := genesisHeader
	td := new(big.Int).Set(genesisHeader.Difficulty)

	numSealed := 0

	for i := 1; i <= n; i++ {
		currHeader := &types.Header{
			ParentHash: lastHeader.Hash(),
			UncleHash: common.HexToHash("0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"),
			Coinbase: common.HexToAddress("0x0"),
			Root: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			TxHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			ReceiptHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			Bloom: types.BytesToBloom(common.FromHex("0x0")),
			Difficulty: big.NewInt(minimumDifficulty),
			Number: big.NewInt(0).Add(lastHeader.Number, big.NewInt(1)),
			GasLimit: uint64(3141592),
			GasUsed: uint64(0),
			Time: lastHeader.Time + uint64(13),
			Extra: make([]byte, 0),
		}
		lastHeader = currHeader
		td.Add(td, currHeader.Difficulty)

		results := make(chan *types.Block)
		stop := make(chan struct{})
		err = engine.Seal(nil, types.NewBlockWithHeader(currHeader), results, stop)
		sealedBlock := <-results
		sealedHeader := sealedBlock.Header()
		if err != nil {
			fmt.Println("Could not seal block with header:")
			fmt.Println(currHeader)
			fmt.Println("err =", err)
			return err
		}

		//headers = append(headers, sealedHeader)

		rawdb.WriteTd(batch, sealedHeader.Hash(), sealedHeader.Number.Uint64(), td)
		rawdb.WriteHeader(batch, sealedHeader)

		//Print progress statistics
		numSealed++
		if numSealed % (n/100) == 0 {
			fmt.Print(numSealed/(n/100), "\b%... ")
		}
	}
	fmt.Println("")

	// Write batch to disk
	err = batch.Write()
	if err != nil {
		fmt.Println("Could not write chain to disk")
		fmt.Println("err =", err)
		return err
	}

	fmt.Println("Created chain of", n, "blocks and stored to disk")
	return nil
}
