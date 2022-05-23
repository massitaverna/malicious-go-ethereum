package bridge

import "net"
import "math/big"
import "github.com/ethereum/go-ethereum/ethdb"
import "github.com/ethereum/go-ethereum/attack/msg"
import "github.com/ethereum/go-ethereum/attack/utils"

const (
	ADDR = "localhost"
	PORT = "45678"
)

var conn net.Conn
var incoming chan byte
var outgoing chan byte
var quitCh chan struct{}
var attackPhase utils.AttackPhase
var mustCheatAboutTd bool


func Initialize(id string) error {
	conn, err := net.Dial("tcp", ADDR+":"+PORT)
	if err != nil {
		log("Could not connect to orchestrator")
		log("err =", err)
		return err
	}

	_, err = conn.Write([]byte(id))
	if err != nil {
		log("Could not send node's ID to orchestrator")
		log("err =", err)
		return err
	}

	attackPhase = utils.PredictionPhase
	mustCheatAboutTd = false

	incoming = make(chan byte)
	outgoing = make(chan byte)
	go listenLoop(conn, incoming, quitCh)
	//go sendLoop(conn, outgoing)

	log("Initialized bridge")
	return nil
}

func NotifyDrop() error {
	return nil

	select {
	case <-quitCh:
		return utils.BridgeClosedErr

	default:
		err := sendMessage(msg.PeerDropped)
		if err != nil {
			log("Could not notify peer drop")
		}
		return err
	}
}

func NotifyNewBatchRequest() error {
	return nil

	select {
	case <-quitCh:
		return utils.BridgeClosedErr
		
	default:
		err := sendMessage(msg.GotNewBatchRequest)
		if err != nil {
			log("Could not notify new batch request")
		}
		return err
	}
}

func DoingPrediction() bool {
	select {
	case <-quitCh:
		fatal(utils.BridgeClosedErr, "Could not determine if attack phase is prediction")
		return false
		
	default:
		return (attackPhase==utils.PredictionPhase)
	}
}

func GetChainDatabase(chainType utils.ChainType) ethdb.Database {
	select {
	case <-quitCh:
		fatal(utils.BridgeClosedErr, "Could not serve custom chain")
		return nil
		
	default:
		db, err := getChainDatabase(chainType)
		if err != nil {
			fatal(err, "Could not serve custom chain")
		}
		return db
	}
}

func CheatAboutTd() (*big.Int, bool, error) {
	select {
	case <-quitCh:
		return utils.HigherTd, false, utils.BridgeClosedErr
		
	default:
		return utils.HigherTd, mustCheatAboutTd, nil
	}
}

func Close() {
	close(quitCh)
	close(incoming)
	close(outgoing)
	conn.Close()
}

func handleMessages() {
	for {
		select {
		case <-quitCh: // Not very useful for now as we close quitCh only when we have a socket reading error,
					   // in listenLoop()
			return

		default:
			message, more := <-incoming
			if !more {
				return
			}
			switch message {
			case msg.NextPhase:
				attackPhase += 1
			case msg.EnableCheatAboutTd:
				mustCheatAboutTd = true
			case msg.DisableCheatAboutTd:
				mustCheatAboutTd = false
			}
		}
	}
}

func sendMessage(message byte) error {
	buf := []byte{1}					// Every message has length 1 byte
	buf = append(buf, message)
	n, err := conn.Write(buf)
	if err != nil {
		return err
	}
	if n < len(buf) {
		return utils.PartialSendErr
	}
	return nil
}

