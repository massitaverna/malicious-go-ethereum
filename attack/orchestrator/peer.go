package orchestrator

import "fmt"
import "time"
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
		timestamp := time.Now().Format("[01-02|15:04:05.000]")
		fmt.Println(timestamp + " FOR", p.id)
		select {
		case <-quitCh:
			p.conn.Close()
			return
		case <-p.stop:
			p.conn.Close()
			return
		default:
		}

		fmt.Println("Listening ", p.id)
		n, err := p.conn.Read(buf[bufLength:])
		for n==0 {
			n, err = p.conn.Read(buf[bufLength:])
		}
		fmt.Println("Listened", n, " bytes", p.id)
		bufLength += uint32(n)
		fmt.Println("Buffer: ", buf[:bufLength], p.id)
		fmt.Println("bufLength: ", bufLength, p.id)
		fmt.Println("Buffer (first 20 bytes): ", buf[:20], )

		if err != nil {
			fmt.Println("Error receiving message")
			fmt.Println("err =", err)
			//errc <- err
			return
		}
		for bufLength >= 4 {
			msgLength := binary.BigEndian.Uint32(buf[:4])
			fmt.Println("msgLength:", msgLength, p.id)
			if bufLength < msgLength + 4 {
				break
			}
			messageAsBytes := buf[:4+msgLength]
			//buf = buf[4+msgLength:]
			temp := make([]byte, 1024)
			copy(temp, buf[4+msgLength:])
			buf = temp
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