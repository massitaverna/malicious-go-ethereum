package buildchain

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"encoding/binary"
	"math/rand"
	"crypto/ecdsa"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/ethereum/go-ethereum/core/rawdb"
	ethdbLeveldb "github.com/ethereum/go-ethereum/ethdb/leveldb"
	"github.com/ethereum/go-ethereum/attack/utils"
)

var chainDbPath string
var dbIdPath string
var ethashDatasetDir string
var vmcfg = vm.Config{EnablePreimageRecording: false}
var privkey *ecdsa.PrivateKey
var pubkey ecdsa.PublicKey
var coinbase common.Address

func initCoinbase() error {
	var err error
	privkey, err = crypto.GenerateKey()
	if err != nil {
		return err
	}
	coinbase = crypto.PubkeyToAddress(privkey.PublicKey)
	return nil
}

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

func BuildChain(chainType utils.ChainType, length int, overwrite bool, numAccts int, debug bool) error {
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

	db.Close()

	chainDb, err := rawdb.NewLevelDBDatabase(chainDbPath, 0, 0, "", false)
	if err != nil {
		fmt.Println("Could not open rawdb database at", chainDbPath)
		return err
	}

	success := false
	defer func() {
		err = chainDb.Close()
		if err != nil {
			fmt.Println("Could not close rawdb database properly")
			fmt.Println("err =", err)
		}
		if !success {
			err = resetDatabase(chainDbPath)
			if err != nil {
				fmt.Println("err =", err)
			}
		} else {
			fmt.Println("\nCreated chain of", length, "blocks and stored to disk")
		}
	}()

	// Enable geth logs
	if debug {
		log.Root().SetHandler(log.LvlFilterHandler(log.LvlDebug, log.StdoutHandler))
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
		DatasetsOnDisk:   20,		// Number of DAGs (one per epoch) to store on disk
		DatasetsLockMmap: false,
	}

	engine := ethash.New(ethashConfig, nil, true)
	fmt.Println("If ethash DAG is absent, it will be generated now. This may take few minutes.")


	//dbWrapper := ethdbLeveldb.NewSimple(db)
	//batch := dbWrapper.NewBatch()				// Batch object to write to database

	coinbase = common.HexToAddress("0x0000000000000000000000000000000000000001")
	if chainType == utils.TrueChain {
		if err := initCoinbase(); err != nil {
			fmt.Println("Could not create ecdsa key")
			return err
		}
	}
	fmt.Println("Using coinbase", coinbase)
	numAccounts = uint64(numAccts)

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
		GasLimit: uint64(0x4c4b40),	//5,000,000
		GasUsed: uint64(0),
		Time: uint64(0),
		Extra: params.DAOForkBlockExtra,
		MixDigest: common.HexToHash("0x0"),
		Nonce: types.EncodeNonce(uint64(0x2763ab980cd417ef)),
		BaseFee: big.NewInt(params.InitialBaseFee),
	}

	chainConfig := params.MainnetChainConfig
	if chainType != utils.PredictionChain {			// Both the true chain and the fake segment need to be at
													// the last Ethereum fork, in order for the simulation to
													// be as real as possible.
		chainConfig = &params.ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      big.NewInt(0),
			DAOForkBlock:        big.NewInt(0),
			DAOForkSupport:      true,
			EIP150Block:         big.NewInt(0),
			//EIP150Hash:          common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
			EIP155Block:         big.NewInt(0),
			EIP158Block:         big.NewInt(0),
			ByzantiumBlock:      big.NewInt(0),
			ConstantinopleBlock: big.NewInt(0),
			PetersburgBlock:     big.NewInt(0),
			IstanbulBlock:       big.NewInt(0),
			MuirGlacierBlock:    big.NewInt(0),
			BerlinBlock:         big.NewInt(0),
			LondonBlock:         big.NewInt(0),
			ArrowGlacierBlock:   big.NewInt(0),
			//GrayGlacierBlock:    big.NewInt(0),
			Ethash:              new(params.EthashConfig),
		}
	}

	// Create Ethereum state
	stateDir, err := tempDir(8)
	if err != nil {
		return err
	}
	stateLeveldb, err := leveldb.OpenFile(stateDir, nil)
	if err != nil {
		fmt.Println("Could not create a state database")
		return err
	}
	stateLeveldbWrapper := rawdb.NewDatabase(ethdbLeveldb.NewSimple(stateLeveldb))
	stateDb := state.NewDatabase(stateLeveldbWrapper)
	ethState, err := state.New(genesisHeader.Root, stateDb, nil)
	if err != nil {
		fmt.Println("Could not create an Ethereum state")
		return err
	}

	//headers := []*types.Header{genesisHeader}
	lastHeader := genesisHeader
	td := new(big.Int).Set(genesisHeader.Difficulty)

	genesisBlock := types.NewBlockWithHeader(genesisHeader)
	rawdb.WriteTd(chainDb, genesisBlock.Hash(), genesisBlock.NumberU64(), td)
	rawdb.WriteBlock(chainDb, genesisBlock)
	rawdb.WriteCanonicalHash(chainDb, genesisHeader.Hash(), genesisHeader.Number.Uint64())
	rawdb.WriteHeadBlockHash(chainDb, genesisHeader.Hash())

	blockchain, err := core.NewBlockChain(chainDb, nil, chainConfig, engine, vmcfg, nil, nil)
	if err != nil {
		fmt.Println("Could not create a blockchain object")
		return err
	}

	numDone := 1 // Genesis block is already done

	for i := 1; i <= length; i++ {
		currHeader := &types.Header{
			ParentHash: lastHeader.Hash(),
			UncleHash: common.HexToHash("0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"),
			Coinbase: coinbase,
			Root: common.Hash{},
			TxHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			ReceiptHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			Bloom: types.BytesToBloom(common.FromHex("0x0")),
			Difficulty: ethash.CalcDifficulty(chainConfig, lastHeader.Time + uint64(13), lastHeader),
			Number: big.NewInt(0).Add(lastHeader.Number, big.NewInt(1)),
			GasLimit: genesisHeader.GasLimit,
			GasUsed: uint64(0),
			Time: lastHeader.Time + uint64(13),
			Extra: params.DAOForkBlockExtra,
			BaseFee: misc.CalcBaseFee(chainConfig, lastHeader),
		}
		td.Add(td, currHeader.Difficulty)

		var block *types.Block

		// Use the first onlyRewardsBlocks blocks to just generate block rewards,
		// while the following ones to create accounts as well.
		if i <= onlyRewardsBlocks || chainType != utils.TrueChain {
			block, err = engine.FinalizeAndAssemble(blockchain, currHeader, ethState, nil, nil, nil)
		
		// Transfer some wei to many accounts.
		} else {
			numTxs := int(numAccounts) / (length-onlyRewardsBlocks)

			// Include a tx early on in the chain to avoid geth bug about legacy receipts.
			if i == onlyRewardsBlocks + 1 && numTxs == 0 && numAccounts > 0 {
				numTxs++
			}
			if i == length && numAccounts > 0 {
				if numTxs == 0 {
					numTxs--
				}
				numTxs += int(numAccounts) % (length-onlyRewardsBlocks)
			}
			txs, receipts, err := autoTransactions(numTxs, currHeader, blockchain, ethState, chainConfig)
			if err != nil {
				return err
			}
			block, err = engine.FinalizeAndAssemble(blockchain, currHeader, ethState, txs, nil, receipts)
			resetGasPool()
		}
		if err != nil {
			fmt.Println("Could not finalize block", i)
			return err
		}
		// Seal the block if we are not building the prediction chain or
		// we are not in the first 50 blocks of the last full batch
		startOfLastBatch := length - length%utils.BatchSize - utils.BatchSize
		if chainType!=utils.PredictionChain || !(startOfLastBatch < i && i <= startOfLastBatch + 50) {
			results := make(chan *types.Block, 1)
			stop := make(chan struct{})
			err = engine.Seal(nil, block, results, stop)
			if err != nil {
				fmt.Println("Could not seal block with header:")
				fmt.Println(currHeader)
				return err
			}

			block = <-results
		}
		
		if i == 1 {
			fmt.Println("Ethash DAG generated")
		}

		//headers = append(headers, sealedHeader)
		lastHeader = block.Header()

		rawdb.WriteTd(chainDb, block.Hash(), block.NumberU64(), td)
		rawdb.WriteBlock(chainDb, block)
		rawdb.WriteCanonicalHash(chainDb, lastHeader.Hash(), lastHeader.Number.Uint64())


		//Print progress statistics
		numDone++
		if numDone % int((math.Ceil(float64(length)/100))) == 0 {
			fmt.Print(numDone/int(math.Ceil(float64(length)/100)), "%... ")
		}
	}
	rawdb.WriteHeadHeaderHash(chainDb, lastHeader.Hash())

	// Explicit Write() shouldn't be necessary now as chainDb.Close() already flushes to disk.
	/*
	// Write batch to disk
	err = chainDb.db.Write()
	if err != nil {
		fmt.Println("Could not write chain to disk")
		success = false
		return err
	}
	*/

	success = true
	return nil
}

func Export(chainType utils.ChainType, filename string) error {
	if err := setChainDbPath(chainType); err != nil {
		return err
	}
	err := utils.Export(chainDbPath, filename)
	if err != nil {
		fmt.Println("Export of chain at " + chainDbPath + " to file " + filename + " failed")
		return err
	}
	fmt.Println("Exported " + chainType.String() + " chain to " + filename)
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

func tempDir(n int) (string, error) {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, n)
    for i := range b {
        b[i] = letters[rand.Intn(len(letters))]
    }
    dname, err := os.MkdirTemp("", string(b))
    if err != nil {
    	fmt.Println("Could not create temporary directory")
    	return "", err
    }
    return dname, nil
}
