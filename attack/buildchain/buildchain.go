package buildchain

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"time"
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
	"github.com/ethereum/go-ethereum/ethdb"
	ethdbLeveldb "github.com/ethereum/go-ethereum/ethdb/leveldb"
	//dircopy "github.com/otiai10/copy"

	"github.com/ethereum/go-ethereum/attack/utils"
)

var chainDbPath string
var dbIdPath string
var ethashDatasetDir string
var vmcfg = vm.Config{EnablePreimageRecording: false}
var privkey *ecdsa.PrivateKey
var pubkey ecdsa.PublicKey
var coinbase common.Address
var simulation = true
var sealsMap map[int]bool
var timestampDeltasMap map[int]int


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

func makeMaps() {
	sealsMap = make(map[int]bool)
	timestampDeltasMap = make(map[int]int)
}

func SetRealMode() {
	simulation = false
}

func BuildChain(chainType utils.ChainType, length int, overwrite bool, numAccts int, ghostAttack, debug bool, buildResults chan types.Blocks) error {
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

	var chainDb ethdb.Database
	var err error
	// For the fake chain, the database directory is already set up by the orchestrator
	if chainType != utils.FakeChain {
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

		chainDb, err = rawdb.NewLevelDBDatabase(chainDbPath, 0, 0, "", false)
	} else {
		freezer := chainDbPath + string(os.PathSeparator) + "ancient"
		chainDb, err = rawdb.NewLevelDBDatabaseWithFreezer(chainDbPath, 0, 0, freezer, "", false)
	}

	if err != nil {
		fmt.Println("Could not open rawdb database at", chainDbPath)
		return err
	}

	//TODO: REMOVE
	/*
	hnumber := uint64(490882)
	hhash := rawdb.ReadCanonicalHash(chainDb, hnumber)
	fmt.Println("Hash", hhash)
	originalHead = rawdb.ReadHeader(chainDb, hhash, hnumber)
	*/

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
		//Difficulty: big.NewInt(65536),
		Difficulty: params.MinimumDifficulty,
		Number: big.NewInt(0),
		GasLimit: uint64(0x4c4b40),	//5,000,000
		GasUsed: uint64(0),
		Time: uint64(0), //Later on, set it to uint64(1646002800)
		Extra: params.DAOForkBlockExtra,
		MixDigest: common.HexToHash("0x0"),
		Nonce: types.EncodeNonce(uint64(0x2763ab980cd417ef)),
		//BaseFee: big.NewInt(params.InitialBaseFee),
	}

	chainConfig := params.MainnetChainConfig
	if simulation {
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
			GrayGlacierBlock:    big.NewInt(0),
			Ethash:              new(params.EthashConfig),
		}

		genesisHeader.BaseFee = big.NewInt(params.InitialBaseFee)
	}

	/*
	if chainType == utils.FakeChain {
		genesisHeader = types.CopyHeader(originalHead)
	}
	*/

	// Create/open Ethereum state
	var ethState *state.StateDB
	if chainType != utils.FakeChain {
		stateDir, err := tempDir(8)
		if err != nil {
			return err
		}

		/*
		defer func() {
			//TODO: delete stateDir from /tmp
		}()
		*/
		stateLeveldb, err := leveldb.OpenFile(stateDir, nil)
		if err != nil {
			fmt.Println("Could not create/open state database")
			return err
		}
		stateLeveldbWrapper := rawdb.NewDatabase(ethdbLeveldb.NewSimple(stateLeveldb))
		stateDb := state.NewDatabase(stateLeveldbWrapper)
		ethState, err = state.New(genesisHeader.Root, stateDb, nil)
	} else {
		var root common.Hash
		if ghostAttack {
			root = ghostRoot
		} else {
			root = originalHead.Root
		}
		stateDb := state.NewDatabase(chainDb)
		ethState, err = state.New(root, stateDb, nil)
	}
	if err != nil {
		fmt.Println("Could not create an Ethereum state")
		return err
	}

	//headers := []*types.Header{genesisHeader}

	var lastHeader *types.Header
	var td *big.Int
	if chainType == utils.FakeChain {
		lastHeader = types.CopyHeader(originalHead)
		td = rawdb.ReadTd(chainDb, lastHeader.Hash(), lastHeader.Number.Uint64())
	} else {
		lastHeader = genesisHeader
		td = new(big.Int).Set(genesisHeader.Difficulty)
	}
	if chainType != utils.FakeChain {
		genesisBlock := types.NewBlockWithHeader(genesisHeader)
		rawdb.WriteTd(chainDb, genesisBlock.Hash(), genesisBlock.NumberU64(), td)
		rawdb.WriteBlock(chainDb, genesisBlock)
		rawdb.WriteCanonicalHash(chainDb, genesisHeader.Hash(), genesisHeader.Number.Uint64())
		rawdb.WriteHeadBlockHash(chainDb, genesisHeader.Hash())
	}


	var cacheConfig = &core.CacheConfig{
		TrieCleanLimit: 256,
		TrieDirtyLimit: 256,
		TrieTimeLimit:  5 * time.Minute,
		SnapshotLimit:  0,
		SnapshotWait:   true,
	}
	blockchain, err := core.NewBlockChain(chainDb, cacheConfig, chainConfig, engine, vmcfg, nil, nil)
	if err != nil {
		fmt.Println("Could not create a blockchain object")
		return err
	}

	//TODO: REMOVE true ||
	if /*true ||*/ chainType != utils.FakeChain {
		makeMaps()
	}
	switch (chainType) {
	case utils.PredictionChain:
		startOfLastBatch := length - length%utils.BatchSize - utils.BatchSize
		for i := 1; i <= length; i++ {
			sealsMap[i] = !(startOfLastBatch < i && i <= startOfLastBatch + 50)
			timestampDeltasMap[i] = 13
		}
	case utils.TrueChain:
		for i := 1; i <= length; i++ {
			sealsMap[i] = true
			timestampDeltasMap[i] = 13
		}
	//TODO: REMOVE
	/*
	case utils.FakeChain:
		for i := 1; i <= length; i++ {
			sealsMap[i+490882] = true
			timestampDeltasMap[i+490882] = 13
		}
	*/
	}

	numDone := 1 // Genesis block is already done
	bigOne := big.NewInt(1)
	offset := 0
	if chainType == utils.FakeChain {
		offset = int(originalHead.Number.Uint64())
	}
	if chainType == utils.FakeChain && hashrate >= 0 {
		fmt.Printf("Enforcing hashrate limiting to %d\n", hashrate)
		ethash.SetHashrateLimit(hashrate)
	}
	engine.SetThreads(0)
	resultsCache := make(types.Blocks, 0)

	start := time.Now()
	for i := 1; i <= length; i++ {
		if _, tExists := timestampDeltasMap[i+offset]; !tExists {
			return fmt.Errorf("Timestamp delta value for block number %d does not exist", i+offset)
		}
		currHeader := &types.Header{
			ParentHash: lastHeader.Hash(),
			UncleHash: common.HexToHash("0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"),
			Coinbase: coinbase,
			Root: common.Hash{},
			TxHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			ReceiptHash: common.HexToHash("0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"),
			Bloom: types.BytesToBloom(common.FromHex("0x0")),
			Difficulty: ethash.CalcDifficulty(chainConfig, lastHeader.Time + uint64(timestampDeltasMap[i+offset]), lastHeader),
			Number: big.NewInt(0).Add(lastHeader.Number, bigOne),
			GasLimit: lastHeader.GasLimit,
			GasUsed: uint64(0),
			Time: lastHeader.Time + uint64(timestampDeltasMap[i+offset]),
			Extra: params.DAOForkBlockExtra,
			//BaseFee: misc.CalcBaseFee(chainConfig, lastHeader),
		}
		// Again, we do this always. Later on, we'll see from config file.
		if true || chainType != utils.PredictionChain {
			currHeader.BaseFee = misc.CalcBaseFee(chainConfig, lastHeader)
		}
		td.Add(td, currHeader.Difficulty)

		//fmt.Printf("Start mining %d-th block at difficulty %d\n", i, currHeader.Difficulty.Uint64())
		var block *types.Block

		// Use the first onlyRewardsBlocks blocks to just generate block rewards,
		// while the following ones to create accounts as well.
		if i <= onlyRewardsBlocks || chainType != utils.TrueChain {
			// In this cases, we do something different (see below)
			if !(i <= 2 && chainType == utils.FakeChain && ghostAttack) {
				block, err = engine.FinalizeAndAssemble(blockchain, currHeader, ethState, nil, nil, nil)
			}
		// Transfer some wei to many accounts.
		} else {
			numTxs := int(numAccounts) / (length-onlyRewardsBlocks)
			surplus := int(numAccounts) % (length-onlyRewardsBlocks)

			// In order to have the exact requested amount of TXs in the chain, we include one more TX
			// for each of the first 'surplus' blocks.
			// This also automatically avoids the geth bug about legacy receipts, since we increment
			// numTxs for the FIRST 'surplus' blocks, and not the LAST ones.
			if i <= surplus + onlyRewardsBlocks {
				numTxs++
			}
			txs, receipts, err := autoTransactions(numTxs, currHeader, blockchain, ethState, chainConfig)
			if err != nil {
				return err
			}
			block, err = engine.FinalizeAndAssemble(blockchain, currHeader, ethState, txs, nil, receipts)
			resetGasPool()
		}
		if err != nil {
			fmt.Println("Could not finalize block", i+offset)
			return err
		}

		// Set the stateRoot to the ghostRoot if this is the first block, and make sure the state is not altered
		// here because malicious peers must be able to get/compute it.
		if i == 1 && chainType == utils.FakeChain && ghostAttack {
			ghostNumber := originalHead.Number.Uint64()+1
			ghostHash := rawdb.ReadCanonicalHash(chainDb, ghostNumber)
			ghostBlock := rawdb.ReadBlock(chainDb, ghostHash, ghostNumber)
			if ghostBlock.Root() != ghostRoot {
				fmt.Println("ghostRoot:", ghostRoot)
				fmt.Println("ghostBlock.Root:", ghostBlock.Root)
				return fmt.Errorf("Inconsistency between ghostRoot and ghostBlock")
			}
			fmt.Println("ghostRoot:", ghostRoot)

			tempBody := ghostBlock.Body()
			tempHdr := ghostBlock.Header()
			tempHdr.Time = lastHeader.Time + uint64(timestampDeltasMap[i+offset])
			tempHdr.Difficulty = ethash.CalcDifficulty(chainConfig, lastHeader.Time + uint64(timestampDeltasMap[i+offset]), lastHeader)
			// Arbitrary extra data to make sure ghostBlock is different from canonical one.
			tempHdr.Extra = utils.GhostExtra[:]
			block = types.NewBlockWithHeader(tempHdr).WithBody(tempBody.Transactions, tempBody.Uncles)
			fmt.Printf("Block %d modified as ghost block\n", block.NumberU64())
		}
		// Arbitrary, invalid modification to the state, to show that snap victims do not validate the state.
		// Specifically, we add utils.FakeMoney ether to address utils.AdversaryAddress.
		if i == 2 && chainType == utils.FakeChain && ghostAttack {
			ethState.AddBalance(utils.AdversaryAddress, utils.FakeMoney)
			block, err = engine.FinalizeAndAssemble(blockchain, currHeader, ethState, nil, nil, nil)
			if err != nil {
				fmt.Println("Couldn't finalize block")
				return err
			}
			fmt.Printf("\nMalicious invalid state included into block %d\n", block.NumberU64())
		}
		// Seal the block if we are not building the prediction chain or
		// we are not in the first 50 blocks of the last full batch
		//startOfLastBatch := length - length%utils.BatchSize - utils.BatchSize
		//if chainType!=utils.PredictionChain || !(startOfLastBatch < i && i <= startOfLastBatch + 50) {
		if toSeal, exists := sealsMap[i+offset]; !exists {
			return fmt.Errorf("Seal value for block number %d does not exist", i+offset)
		} else if toSeal {
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

		if buildResults != nil {
			resultsCache = append(resultsCache, block)
			if len(resultsCache) == utils.BatchSize {
				rc := make(types.Blocks, len(resultsCache))
				copy(rc, resultsCache)
				buildResults <- rc
				resultsCache = make(types.Blocks, 0)
			}
		}


		//Print progress statistics
		numDone++
		if numDone % int((math.Ceil(float64(length)/100))) == 0 {
			fmt.Print(numDone/int(math.Ceil(float64(length)/100)), "%... ")
		}
	}
	fmt.Println("Elapsed:", common.PrettyDuration(time.Since(start)))
	rawdb.WriteHeadHeaderHash(chainDb, lastHeader.Hash())
	rawdb.WriteHeadBlockHash(chainDb, lastHeader.Hash())

	if buildResults != nil && len(resultsCache) != 0 {
		buildResults <- resultsCache
	}

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

	ethash.SetHashrateLimit(-1)
	success = true
	return nil
}

func PrebuildDAG(blockNum uint64) error {
	if err := setEthashDatasetDir(); err != nil {
		fmt.Println("Couldn't prebuild DAG for block", blockNum)
		return err
	}

	ethashConfig := ethash.Config{
		DatasetDir:       ethashDatasetDir,
		CacheDir:         "ethash",
		CachesInMem:      2,
		CachesOnDisk:     3,
		CachesLockMmap:   false,
		DatasetsInMem:    1,
		DatasetsOnDisk:   3,		// Number of DAGs (one per epoch) to store on disk
		DatasetsLockMmap: false,
	}
	engine := ethash.New(ethashConfig, nil, true)
	engine.Dataset(blockNum, false)
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
