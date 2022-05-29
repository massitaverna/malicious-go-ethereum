package orchestrator

import "fmt"
import "sync"
import "net"
import "errors"
import "encoding/binary"
import "github.com/ethereum/go-ethereum/attack/msg"
//import "github.com/ethereum/go-ethereum/attack/utils"

type Peer struct {
	id string
	conn net.Conn
	stop chan struct{}
}

/*
func (p *Peer) distributeChain(chainType utils.ChainType) {

}
*/

type PeerSet struct {
	peers map[string]*Peer
	masterPeer *Peer
	mutex sync.Mutex
}

func (ps *PeerSet) add(id string, p *Peer) {
	ps.mutex.Lock()
	ps.peers[id] = p
	ps.mutex.Unlock()
}

func (ps *PeerSet) remove(id string) {
	ps.mutex.Lock()
	close(ps.peers[id].stop)
	ps.peers[id] = nil
	ps.mutex.Unlock()
}

func (ps *PeerSet) len() int {
	ps.mutex.Lock()
	n := len(ps.peers)
	ps.mutex.Unlock()
	return n
}



type peerMessage struct {
	peer *Peer
	message *msg.Message
}

func (p *Peer) readLoop(incoming chan *peerMessage, quitCh chan struct{}, errc chan error) {
	bufLength := uint32(0)
	buf := make([]byte, 1024)

	for {
		select {
		case <-quitCh:
			p.conn.Close()
			return
		case <-p.stop:
			p.conn.Close()
			return
		default:
		}

		n, err := p.conn.Read(buf[bufLength:])
		bufLength += uint32(n)

		if err != nil {
			fmt.Println("Error receiving message")
			fmt.Println("err =", err)
			//errc <- err
			return
		} else if bufLength < 4 {
			continue
		} else {

			msgLength := binary.BigEndian.Uint32(buf[:4])
			if bufLength < msgLength + 4 {
				continue
			}
			messageAsBytes := buf[:4+msgLength]
			buf = buf[4+msgLength:]
			bufLength -= 4 + msgLength
			message := msg.Decode(messageAsBytes)
			incoming <- &peerMessage{
				peer: p,
				message: message,
			}
		}
	}
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

func (ps *PeerSet) close() {
	for _, peer := range ps.peers {
		peer.conn.Close()
	}
}