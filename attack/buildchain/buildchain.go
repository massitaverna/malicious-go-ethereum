package buildchain

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"encoding/binary"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/ethereum/go-ethereum/core/rawdb"
	ethdbLeveldb "github.com/ethereum/go-ethereum/ethdb/leveldb"
	"github.com/ethereum/go-ethereum/attack/utils"
)

var chainDbPath string
var dbIdPath string
var ethashDatasetDir string


func setChainDbPath(chainType utils.ChainType) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	pathSeparator := string(os.PathSeparator)
	chainDbPath = home + pathSeparator + ".buildchain" + pathSeparator + chainType.GetDir()
	dbIdPath = chainDbPath + pathSeparator + "db_id"
	return nil
}

func setEthashDatasetDir() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	pathSeparator := string(os.PathSeparator)
	ethashDatasetDir = home + pathSeparator + ".buildchain" + pathSeparator + "ethash"
	return nil
}

func BuildChain(chainType utils.ChainType, length int, overwrite bool) error {
	// Create/open database
	if err := setChainDbPath(chainType); err != nil {
		return err
	}

	if overwrite {
		err := resetDatabase(chainDbPath)
		if err != nil {
			return err
		}
	}
	db, err := leveldb.OpenFile(chainDbPath, &opt.Options{ErrorIfExist: true})
	if err == os.ErrExist {
		dbId, errf := loadId(dbIdPath)
		if errf != nil {
			fmt.Println("Could not load database ID of an already existing database")
			return errf
		}
		if dbId == computeId(length) {
			fmt.Println("The chain is already built and ready to use")
			return nil
		} else {
			fmt.Println("Another chain is stored at " + chainDbPath)
			fmt.Println("To override it, run your command with the flag --overwrite")
			return os.ErrExist
		}
	}
	if err != nil {
		fmt.Println("An error happened opening the database")
		return err
	}

	err = storeId(dbIdPath, computeId(length))
	if err != nil {
		fmt.Println("Could not store ID of newly created database")
		return err
	}

	success := true
	defer func() {
		db.Close()
		if !success {
			resetDatabase(chainDbPath)
		}
	}()

	// Enable geth logs
	if utils.Debug {
		log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StdoutHandler))
	}

	// Create consensus engine
	if err := setEthashDatasetDir(); err != nil {
		return err
	}

	ethashConfig := ethash.Config{
		DatasetDir:       ethashDatasetDir,
		CacheDir:         "ethash",
		CachesInMem:      2,
		CachesOnDisk:     3,
		CachesLockMmap:   false,
		DatasetsInMem:    1,
		DatasetsOnDisk:   2,
		DatasetsLockMmap: false,
	}

	engine := ethash.New(ethashConfig, nil, true)
	fmt.Println("If ethash DAG is absent, it will be generated now. This may take few minutes.")


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
		Difficulty: params.MinimumDifficulty,
		Number: big.NewInt(0),
		GasLimit: uint64(3141592),
		GasUsed: uint64(0),
		Time: uint64(0),
		Extra: make([]byte, 0),
		MixDigest: common.HexToHash("0x0"),
		Nonce: types.EncodeNonce(uint64(0x2763ab980cd417ef)),
		//BaseFee: big.NewInt(1000000000),
	}


	//headers := []*types.Header{genesisHeader}
	lastHeader := genesisHeader
	td := new(big.Int).Set(genesisHeader.Difficulty)

	genesisBlock := types.NewBlockWithHeader(genesisHeader)
	rawdb.WriteTd(batch, genesisBlock.Hash(), genesisBlock.NumberU64(), td)
	rawdb.WriteBlock(batch, genesisBlock)
	rawdb.WriteCanonicalHash(batch, genesisHeader.Hash(), genesisHeader.Number.Uint64())

	numDone := 1 // Genesis block is already done

	for i := 1; i <= length; i++ {
		currHeader := &types.Header{
			ParentHash: lastHeader.Hash(),
			UncleHash: common.HexToHash("0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"),
			Coinbase: common.HexToAddress("0x0"),
			Root: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			TxHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			ReceiptHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			Bloom: types.BytesToBloom(common.FromHex("0x0")),
			Difficulty: params.MinimumDifficulty,
			Number: big.NewInt(0).Add(lastHeader.Number, big.NewInt(1)),
			GasLimit: uint64(3141592),
			GasUsed: uint64(0),
			Time: lastHeader.Time + uint64(13),
			Extra: make([]byte, 0),
			//BaseFee: lastHeader.BaseFee,
		}
		td.Add(td, currHeader.Difficulty)

		block := types.NewBlockWithHeader(currHeader)

		// Seal the block if we are not building the prediction chain or
		// we are not in the first 50 blocks of the last full batch
		startOfLastBatch := length - length%utils.BatchSize - 2*utils.BatchSize		// Oracle batch is the
																					// second-to-last because last
																					// is fully invalid
		if chainType!=utils.PredictionChain || !(startOfLastBatch < i && i <= startOfLastBatch + 50 ||
												 i > length-utils.BatchSize) {
			results := make(chan *types.Block)
			stop := make(chan struct{})
			err = engine.Seal(nil, block, results, stop)
			if err != nil {
				fmt.Println("Could not seal block with header:")
				fmt.Println(currHeader)
				success = false
				return err
			}

			block = <-results
		}
		
		if i == 1 {
			fmt.Println("Ethash DAG generated")
		}

		//headers = append(headers, sealedHeader)
		lastHeader = block.Header()

		rawdb.WriteTd(batch, block.Hash(), block.NumberU64(), td)
		rawdb.WriteBlock(batch, block)
		rawdb.WriteCanonicalHash(batch, lastHeader.Hash(), lastHeader.Number.Uint64())


		//Print progress statistics
		numDone++
		if numDone % int((math.Ceil(float64(length)/100))) == 0 {
			fmt.Print(numDone/int(math.Ceil(float64(length)/100)), "%... ")
		}
	}
	fmt.Println("")

	rawdb.WriteHeadHeaderHash(batch, lastHeader.Hash())

	// Write batch to disk
	err = batch.Write()
	if err != nil {
		fmt.Println("Could not write chain to disk")
		success = false
		return err
	}

	fmt.Println("Created chain of", length, "blocks and stored to disk")
	return nil
}

func computeId(length int) uint64 {
	return uint64(length)
}

func storeId(path string, id uint64) error {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(id))
	err := os.WriteFile(path, buf, 0644)
	return err
}

func loadId(path string) (uint64, error) {
	dbId, err := os.ReadFile(dbIdPath)
	if err != nil {
		return 0, err
	}

	return binary.BigEndian.Uint64(dbId), nil
}

func resetDatabase(path string) error {
	err := os.RemoveAll(path)
	if err != nil {
		fmt.Println("An unexpected error occurred")
		fmt.Println("You may want to delete " + path + "manually")
	}
	return err
}
