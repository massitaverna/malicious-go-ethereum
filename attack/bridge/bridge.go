package bridge

import "net"
import "os"
import "github.com/ethereum/go-ethereum/attack/msg"

const (
	ADDR = "localhost"
	PORT = "45678"
)

var conn net.Conn


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

	log("Initialized bridge")
	return nil
}

func NotifyDrop() {
	_, err := conn.Write(msg.PeerDropped)
	if err != nil {
		log("Could not notify peer drop")
		log("err =", err)
		log("Exiting")
		os.Exit(1)
	}
}

func NotifyNewBatchRequest() {
	_, err := conn.Write(msg.GotNewBatchRequest)
	if err != nil {
		log("Could not notify new batch request")
		log("err =", err)
		log("Exiting")
		os.Exit(1)
	}
}

