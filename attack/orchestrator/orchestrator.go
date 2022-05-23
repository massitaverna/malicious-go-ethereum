package orchestrator

import "fmt"
import "net"
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
}

func New(errc chan error) *Orchestrator {
	o := &Orchestrator {
		peerset: &PeerSet{
			peers: make(map[string]*Peer),
		},
		quitCh: make(chan struct{}),		// Useless for now as termination upon success/fail is signaled
											// through errc channel
		errc: errc,
	}

	return o
}

func (o *Orchestrator) Start() {
	go o.addPeers()
	go o.leadAttack()
}

func (o *Orchestrator) addPeers() {
	l, err := net.Listen("tcp", ADDR+":"+PORT)
	if err != nil {
		fmt.Println("Error listening on the network")
		o.errc <- err
		return
	}
	defer l.Close()
	defer close(o.quitCh)

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
		}

		o.peerset.add(peerId, peer)
		fmt.Println("Peer " + peerId + " joined (numPeers:", o.peerset.len(), "\b)")
	}

}

func (o *Orchestrator) leadAttack() {
	err := o.sendAll(msg.EnableCheatAboutTd)
	if err != nil {
		fmt.Println("Error enabling cheating about TD on peers")
		o.errc <- err
	}

}

func (o *Orchestrator) Wait() {
	<-o.quitCh
	return
}

func (o *Orchestrator) send(p *Peer, message byte) error {
	buf := []byte{1}						// Every message has length 1 byte
	buf = append(buf, message)
	n, err := p.conn.Write(buf)
	if err != nil {
		return err
	}
	if n < len(buf) {
		return utils.PartialSendErr
	}

	return nil
}

func (o *Orchestrator) sendAll(message byte) error {
	for _, peer := range o.peerset.peers {
		if err := o.send(peer, message); err != nil {
			return err
		}
	}

	return nil
}

func (o *Orchestrator) close() {
	return
}


