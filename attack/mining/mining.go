package mining

import (
	//"fmt"
	"net/http"
	//"math/rand"
	"encoding/json"
	"bytes"
	"strconv"
	"github.com/ethereum/go-ethereum/log"
)

func GetHonestHashrate(address string) (int64, error) {
	request := map[string]string{"jsonrpc": "2.0", "method": "eth_hashrate", "id":"request_for_hashrate"}
    json_data, _ := json.Marshal(request)
	resp, err := http.Post(address, "application/json", bytes.NewBuffer(json_data))
	if err != nil {
		log.Error("Could not connect for hashrate retrieval", "address", address, "err", err)
		return -1, err
	}

	response := make(map[string]string)
	//err = json.Unmarshal(resp.Body, response)
	json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		log.Error("Could not decode RPC response", "resp", resp.Body, "err", err)
		return -1, err
	}
	hashrate, err := strconv.ParseInt(response["result"], 16, 64)
	if err != nil {
		log.Error("Could not convert hashrate string to integer", "string", response["result"], "err", err)
		return -1, err
	}

	return hashrate, nil
}

/*
func PrintRandomOutput() {
	seed := int64(4069594212570144568)
	prng := rand.New(rand.NewSource(seed))
	skip := 16*10 + 14
	for i:=0; i < skip; i++ {
		prng.Intn(100)
	}
	fmt.Println(prng.Intn(100))
}
*/