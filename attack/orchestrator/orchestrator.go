package orchestrator

import "fmt"
import "net"

const (
	ADDR = "localhost"
	PORT = "45678"
)

type Orchestrator struct {
	peerset *PeerSet
	quitCh chan struct{}
}

func New() *Orchestrator {
	o := &Orchestrator {
		peerset: &PeerSet{
			peers: make(map[string]*Peer),
		},
		quitCh: make(chan struct{}),
	}

	return o
}

func (o *Orchestrator) Start() {
	go o.addPeers()
}

func (o *Orchestrator) addPeers() error {
	l, err := net.Listen("tcp", ADDR+":"+PORT)
	if err != nil {
		fmt.Println("Error listening on the network:", err)
		return err
	}
	defer l.Close()
	defer close(o.quitCh)

	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting new connection", err)
			continue
		}

		buf := make([]byte, 8)
		_, err = conn.Read(buf)
		if err != nil {
			fmt.Println("Error receiving peer's ID", err)
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

func (o *Orchestrator) Wait() {
	<-o.quitCh
	return
}
func (o *Orchestrator) close() {
	return
}


