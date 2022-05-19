package orchestrator

import "sync"
import "net"
import "errors"

type Peer struct {
	id string
	conn net.Conn
}

type PeerSet struct {
	peers map[string]*Peer
	mutex sync.Mutex
}

func (ps *PeerSet) add(id string, p *Peer) {
	ps.mutex.Lock()
	ps.peers[id] = p
	ps.mutex.Unlock()
}

func (ps *PeerSet) len() int {
	ps.mutex.Lock()
	n := len(ps.peers)
	ps.mutex.Unlock()
	return n
}

func (p *Peer) recvOracleBit() (bool, error) {
	buf := make([]byte, 1)
	_, err := p.conn.Read(buf)
	if err != nil {
		return false, err
	}

	switch buf[0] {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, errors.New("invalid oracle reply")
	}
}