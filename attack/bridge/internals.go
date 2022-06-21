package bridge

import "os"
import "net"
import "encoding/binary"
import dircopy "github.com/otiai10/copy"
import "github.com/ethereum/go-ethereum/core/rawdb"
import "github.com/ethereum/go-ethereum/ethdb"
import "github.com/ethereum/go-ethereum/core/types"
import "github.com/ethereum/go-ethereum/attack/utils"


var databases map[utils.ChainType]ethdb.Database

func getChainDatabase(chainType utils.ChainType) (ethdb.Database, error) {
	if databases == nil {
		databases = make(map[utils.ChainType]ethdb.Database)
	}
	if databases[chainType] != nil {
		return databases[chainType], nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		log("Could not find user home directory")
		return nil, err
	}
	pathSeparator := string(os.PathSeparator)
	srcPath := home + pathSeparator + ".buildchain" + pathSeparator + chainType.GetDir()
	dstPath := "datadir" + pathSeparator +  "mgeth" + pathSeparator + chainType.GetDir()
	err = dircopy.Copy(srcPath, dstPath, dircopy.Options{
		OnDirExists: func (string, string) dircopy.DirExistsAction {
			return dircopy.Replace
		},
	})
	if err != nil {
		log("Could not copy", chainType, "chain to local directory")
		return nil, err
	}

	db, err := rawdb.NewLevelDBDatabase(dstPath, 0, 0, "", false)
	if err != nil {
		log("Could not load ", chainType, " chain database")
		return nil, err
	}
	databases[chainType] = db
	return db, nil
}

/*
func getHigherHeadAndPivot(chainType utils.ChainType) (*types.Header, *types.Header) {
	head := latest(chainType)
	pivot := latest(chainType)
	head.Number = big.NewInt(head.Number.Uint64() + 88)
	head.Time += 13*88
	pivot.Number = big.NewInt(pivot.Number.Uint64() + 24)
	pivot.Time += 13*24

	return head, pivot
}
*/

func latest(chainType utils.ChainType) *types.Header {
	db, err := getChainDatabase(chainType)
	if err != nil {
		fatal(err, "Could not get latest block of", chainType, "chain")
	}
	hash := rawdb.ReadHeadHeaderHash(db)
	number := rawdb.ReadHeaderNumber(db, hash)
	header := rawdb.ReadHeader(db, hash, *number)
	return header
}

/*
func getBlockChain(chainType utils.ChainType) {
	chainDb := databases[chainType]
	config := ethconfig.Defaults
	chainConfig, _, genesisErr := core.SetupGenesisBlockWithOverride(chainDb, config.Genesis, config.OverrideArrowGlacier, config.OverrideTerminalTotalDifficulty)
	// '_' above was 'genesisHash'
	if genesisErr != nil {
		return genesisErr
	}
	var (
		vmConfig = nil
		cacheConfig = &core.CacheConfig{
			TrieCleanLimit:      config.TrieCleanCache,
			//TrieCleanJournal:    stack.ResolvePath(config.TrieCleanCacheJournal),
			TrieCleanJournal:    config.TrieCleanCacheJournal,
			TrieCleanRejournal:  config.TrieCleanCacheRejournal,
			TrieCleanNoPrefetch: config.NoPrefetch,
			TrieDirtyLimit:      config.TrieDirtyCache,
			TrieDirtyDisabled:   config.NoPruning,
			TrieTimeLimit:       config.TrieTimeout,
			SnapshotLimit:       config.SnapshotCache,
			Preimages:           config.Preimages,
		}
	)
	eth.blockchain, err = core.NewBlockChain(databases[chainType], cacheConfig, chainConfig, eth.engine, vmConfig, eth.shouldPreserve, &config.TxLookupLimit)
}
*/

func createMgethDirIfMissing() error {
	_, err := os.Stat("datadir")
	if os.IsNotExist(err) {
		log("Could not find datadir/ in current working directory")
		return err
	}
	err = os.Mkdir("datadir/mgeth", 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}
	return nil
}

func readLoop(conn net.Conn, incoming chan []byte, quitCh chan struct{}) {
	bufLength := uint32(0)
	buf := make([]byte, 1024)

	for {
		select {
		case <-quitCh:
			log("Quitting read loop")
			return
		default:
		}

		n, err := conn.Read(buf[bufLength:])
		bufLength += uint32(n)

		if err != nil {
			fatal(err, "Error receiving message")
		}

		for bufLength >= 4 {
			msgLength := binary.BigEndian.Uint32(buf[:4])
			if bufLength < msgLength + 4 {
				break
			}
			msg := buf[:4+msgLength]
			//buf = buf[4+msgLength:]
			temp := make([]byte, 1024)
			copy(temp, buf[4+msgLength:])
			buf = temp
			bufLength -= 4 + msgLength
			incoming <- msg
		}
	}
}

func fatal(err error, a ...interface{}) {
	log(a...)
	log("err =", err)
	log("Exiting")
	os.Exit(1)
}