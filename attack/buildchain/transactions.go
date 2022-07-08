package buildchain

import (
	"fmt"
	"math/big"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/state"
	//"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	//"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/attack/utils"


)

const (
	ranges = uint64(16)
	//gasForSimpleTx = uint64(21000)
)

var (
	numAccounts = uint64(0)
	gasPool *core.GasPool
	nonce = uint64(0)
	bigZero = big.NewInt(0)
	bigTen = big.NewInt(10)
	oneWei = big.NewInt(1)
	//maxFeePerGas = big.NewInt(100)
	onlyRewardsBlocks = 1
)

func accountsPerRange() uint64 {
	res := numAccounts/ranges
	/*
	if res == 0 {
		return 1
	}
	*/
	return res
}

func accountsOutOfRanges() uint64 {		// Returns the accounts that still need to be created after
										// creating accountsPerRange() accounts in each range.
	return numAccounts % ranges
}

func resetGasPool() {
	gasPool = nil
}

func transfer(from, to, coinbase common.Address, header *types.Header, bc *core.BlockChain, statedb *state.StateDB, chainConfig *params.ChainConfig) (*types.Transaction, *types.Receipt, error) {
	if gasPool == nil {
		gasPool = new(core.GasPool).AddGas(header.GasLimit)
	}

	baseFee := header.BaseFee
	/*
	var max *big.Int
	if maxFeePerGas.Cmp(baseFee) > 0 {
		max = maxFeePerGas
	} else {
		max = baseFee
	}
	*/

	txData := &types.DynamicFeeTx {
		ChainID: big.NewInt(1),
		Nonce: nonce,
		GasTipCap: bigZero,
		GasFeeCap: baseFee,
		Gas: params.TxGas,
		To: &to,
		Value: oneWei,
	}
	signer := types.MakeSigner(chainConfig, header.Number)
	tx, err := types.SignNewTx(privkey, signer, txData)
	if err != nil {
		fmt.Println("Could not sign transaction")
		return nil, nil, err
	}

	fmt.Println("Trying tx, nonce=", nonce, "gasPool=", gasPool, "GasUsed=", header.GasUsed, "gasLimit=", header.GasLimit)
	receipt, err := core.ApplyTransaction(chainConfig, bc, &coinbase, gasPool, statedb, header, tx, &header.GasUsed, vmcfg)
	if err != nil {
		fmt.Println("Could not apply transaction\t\t", "headerNumber=", header.Number, "nonce=", nonce, "to=", to)
		return nil, nil, err
	} else if receipt.Status == types.ReceiptStatusFailed {
		fmt.Println("Transaction failed\t\t", "headerNumber=", header.Number, "nonce=", nonce, "to=", to)
		return nil, nil, utils.StateError
	}
	nonce++
	return tx, receipt, nil
}

func autoTransactions(howMany int, header *types.Header, bc *core.BlockChain, statedb *state.StateDB, chainConfig *params.ChainConfig) ([]*types.Transaction, []*types.Receipt, error) {
	var txs[] *types.Transaction
	var receipts[] *types.Receipt
	bigAcctsPerRange := big.NewInt(int64(accountsPerRange()))

	for i:=0; i < howMany; i++ {
		var to common.Address
		// Accounts to create equally over all ranges
		if nonce < numAccounts - accountsOutOfRanges() {
			bigNonce := big.NewInt(int64(nonce))
			base := new(big.Int).Div(bigNonce, bigAcctsPerRange)
			addrTo := new(big.Int).Mul(utils.RangeOne, base)
			mod := new(big.Int).Mod(bigNonce, bigAcctsPerRange)
			addrTo.Add(addrTo, mod)
			addrTo.Add(addrTo, bigTen)				// Actually, only needed in range 0, as addresses
													// 0x1 - 0x9 are "reserved". However, for symmetry
													// we always add an offset of 10 to addresses, no
													// matter the range.
			to = common.BigToAddress(addrTo)
		// Extra accounts get created in the first range
		} else {
			mod := big.NewInt(int64(nonce % ranges))
			addrTo := new(big.Int).Add(bigAcctsPerRange, mod)
			addrTo.Add(addrTo, bigTen)
			to = common.BigToAddress(addrTo)
		}
		tx, rcpt, err := transfer(coinbase, to, coinbase, header, bc, statedb, chainConfig)
		if err != nil {
			fmt.Println("Automatic transactions failed\t\tnum=", i)
			return nil, nil, err
		}
		txs = append(txs, tx)
		receipts = append(receipts, rcpt)
	}
	return txs, receipts, nil
}