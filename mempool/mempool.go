package mempool

import (
	"context"
	"cosmossdk.io/math"
	"errors"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"
	mempool2 "github.com/cosmos/cosmos-sdk/types/mempool"
	"github.com/cosmos/evm/mempool/miner"
	"github.com/cosmos/evm/mempool/txpool"
	"github.com/cosmos/evm/mempool/txpool/legacypool"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
)

var _ mempool.ExtMempool = &EVMMempool{}
var _ mempool.Iterator = &EVMMempoolIterator{}

type (
	EVMMempool struct {
		/** Keepers **/
		vmKeeper VMKeeperI

		/** Mempools **/
		txPool       *txpool.TxPool
		legacyTxPool *legacypool.LegacyPool
		cosmosPool   mempool.ExtMempool

		/** Utils **/
		txDecoder  sdk.TxDecoder
		blockchain *Blockchain
	}
	EVMMempoolIterator struct {
		/** Mempool Iterators **/
		evmIterator    *miner.TransactionsByPriceAndNonce
		cosmosIterator mempool.Iterator

		/** Chain Params **/
		bondDenom string
	}
)

type EVMMempoolConfig struct {
	TxPool     *txpool.TxPool
	CosmosPool mempool.ExtMempool
}

func NewEVMMempool(ctx func(height int64, prove bool) (sdk.Context, error), vmKeeper VMKeeperI, txDecoder sdk.TxDecoder, config *EVMMempoolConfig) *EVMMempool {
	var txPool *txpool.TxPool
	var cosmosPool mempool.ExtMempool
	bondDenom := "wei"

	if config != nil {
		txPool = config.TxPool
		cosmosPool = config.CosmosPool
	}

	var blockchain *Blockchain
	if txPool == nil {
		//todo: implement blockchain
		blockchain = NewBlockchain(ctx, vmKeeper)
		//todo: custom configs for txpool
		legacyPool := legacypool.New(legacypool.DefaultConfig, blockchain)
		txPoolInit, err := txpool.New(uint64(0), blockchain, []txpool.SubPool{legacyPool})
		if err != nil {
			panic(err)
		}
		txPool = txPoolInit
	}

	if cosmosPool == nil {
		priorityConfig := mempool2.PriorityNonceMempoolConfig[math.Int]{}
		priorityConfig.TxPriority = mempool2.TxPriority[math.Int]{
			GetTxPriority: func(goCtx context.Context, tx sdk.Tx) math.Int {
				cosmosTxFee, ok := tx.(sdk.FeeTx)
				if !ok {
					return math.ZeroInt()
				}
				found, coin := cosmosTxFee.GetFee().Find(bondDenom)
				if !found {
					return math.ZeroInt()
				}
				return coin.Amount
			},
			Compare: func(a, b math.Int) int {
				return a.BigInt().Cmp(b.BigInt())
			},
			MinValue: math.ZeroInt(),
		}
		cosmosPool = mempool2.NewPriorityMempool(priorityConfig)
	}

	if len(txPool.Subpools) != 1 {
		panic("tx pool should contain only one subpool")
	}
	if _, ok := txPool.Subpools[0].(*legacypool.LegacyPool); !ok {
		panic("tx pool should contain only legacypool")
	}
	return &EVMMempool{
		vmKeeper:     vmKeeper,
		txPool:       txPool,
		legacyTxPool: txPool.Subpools[0].(*legacypool.LegacyPool),
		cosmosPool:   cosmosPool,
		txDecoder:    txDecoder,
		blockchain:   blockchain,
	}
}

// GetBlockchain returns the blockchain interface for chain head event notification
func (m *EVMMempool) GetBlockchain() *Blockchain {
	return m.blockchain
}

// getEVMMessage validates the transaction has exactly one message and returns the EVM message if it exists
func (m *EVMMempool) getEVMMessage(tx sdk.Tx) (*evmtypes.MsgEthereumTx, error) {
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return nil, ErrNoMessages
	}
	if len(msgs) != 1 {
		return nil, fmt.Errorf("%w, got %d", ErrExpectedOneMessage, len(msgs))
	}
	ethMsg, ok := msgs[0].(*evmtypes.MsgEthereumTx)
	if !ok {
		return nil, ErrNotEVMTransaction
	}
	return ethMsg, nil
}

func (m *EVMMempool) Insert(ctx context.Context, tx sdk.Tx) error {
	// ASSUMPTION: these are all successful upon CheckTx

	// Try to get EVM message
	ethMsg, err := m.getEVMMessage(tx)
	if err == nil {
		// Insert into EVM pool
		ethTxs := []*ethtypes.Transaction{ethMsg.AsTransaction()}
		errs := m.txPool.Add(ethTxs, true)
		if len(errs) > 0 && errs[0] != nil {
			return errs[0]
		}
		return nil
	}

	// Handle validation errors
	if errors.Is(err, ErrNoMessages) {
		return err
	}
	// For ErrExpectedOneMessage or ErrNotEVMTransaction, treat as cosmos transaction

	// Insert into cosmos pool for non-EVM transactions
	return m.cosmosPool.Insert(ctx, tx)
}

func (m *EVMMempool) InsertInvalidSequence(txBytes []byte) error {
	// ASSUMPTION: these are all failing on ErrInvalidSequence and not another error
	/**
	if tx.type == evm {
		insert into legacy pool
	} else {
		DISCARD: the Cosmos PriorityNonceMempool has no concept of local transaction promotion/demotion,
		so Comet will start picking up invalid transactions from the general queue. Comet will fail to
		pick up transactions with Nonce gaps on RecheckTx.
	}
	*/
	tx, err := m.txDecoder(txBytes)
	if err != nil {
		return err
	}

	var ethTxs []*ethtypes.Transaction
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return fmt.Errorf("%w, got %d", ErrExpectedOneMessage, len(msgs))
	}
	for _, msg := range tx.GetMsgs() {
		ethMsg, ok := msg.(*evmtypes.MsgEthereumTx)
		if ok {
			ethTxs = append(ethTxs, ethMsg.AsTransaction())
			continue
		}
	}
	errs := m.txPool.Add(ethTxs, false) // TODO: proper sync parameters
	if errs != nil {
		if len(errs) != 1 {
			return fmt.Errorf("%w, got %d", ErrExpectedOneError, len(errs))
		}
		return errs[0]
	}
	return nil
}

// setupPendingTransactions extracts common logic for setting up pending transactions
func (m *EVMMempool) setupPendingTransactions(goCtx context.Context, i [][]byte) (*miner.TransactionsByPriceAndNonce, mempool.Iterator, string) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	baseFee := m.vmKeeper.GetBaseFee(ctx)
	var baseFeeUint *uint256.Int
	if baseFee != nil {
		baseFeeUint = uint256.MustFromBig(baseFee)
	}

	pendingFilter := txpool.PendingFilter{
		MinTip:       nil,
		BaseFee:      baseFeeUint,
		BlobFee:      nil,
		OnlyPlainTxs: true,
		OnlyBlobTxs:  false,
	}
	evmPendingTxes := m.txPool.Pending(pendingFilter)
	orderedEVMPendingTxes := miner.NewTransactionsByPriceAndNonce(nil, evmPendingTxes, baseFee)

	cosmosPendingTxes := m.cosmosPool.Select(ctx, i)
	bondDenom := m.vmKeeper.GetParams(ctx).EvmDenom

	return orderedEVMPendingTxes, cosmosPendingTxes, bondDenom
}

func (m *EVMMempool) Select(goCtx context.Context, i [][]byte) mempool.Iterator {
	evmIterator, cosmosIterator, bondDenom := m.setupPendingTransactions(goCtx, i)

	combinedIterator := &EVMMempoolIterator{
		evmIterator:    evmIterator,
		cosmosIterator: cosmosIterator,
		bondDenom:      bondDenom,
	}

	return combinedIterator
}

func (m *EVMMempool) CountTx() int {
	pending, _ := m.txPool.Stats()
	return m.cosmosPool.CountTx() + pending
}

func (m *EVMMempool) Remove(tx sdk.Tx) error {
	// Try to get EVM message
	ethMsg, err := m.getEVMMessage(tx)
	if err == nil {
		// Remove from EVM pool
		m.txPool.Subpools[0].RemoveTx(ethMsg.AsTransaction().Hash(), true, true)
		return nil
	}

	// Handle validation errors
	if errors.Is(err, ErrNoMessages) {
		return err
	}
	// For ErrExpectedOneMessage or ErrNotEVMTransaction, treat as cosmos transaction

	// Remove from cosmos pool for non-EVM transactions
	return m.cosmosPool.Remove(tx)
}

func (m *EVMMempool) SelectBy(goCtx context.Context, i [][]byte, f func(sdk.Tx) bool) {
	evmIterator, cosmosIterator, bondDenom := m.setupPendingTransactions(goCtx, i)

	var combinedIterator mempool.Iterator = &EVMMempoolIterator{
		evmIterator:    evmIterator,
		cosmosIterator: cosmosIterator,
		bondDenom:      bondDenom,
	}

	// todo: ensure that this is not an infinite loop
	// both txPool and PriorityNonceMempool should eventually be exhausted
	// should write tests to make sure of this
	for combinedIterator != nil && f(combinedIterator.Tx()) {
		combinedIterator = combinedIterator.Next()
	}
}

// shouldUseEVM returns true if EVM transaction should be used, false for Cosmos
func (i *EVMMempoolIterator) shouldUseEVM() bool {
	var nextEVMTx *txpool.LazyTransaction
	var evmFee *uint256.Int
	if i.evmIterator != nil {
		nextEVMTx, evmFee = i.evmIterator.Peek()
	}

	var nextCosmosTx sdk.Tx
	if i.cosmosIterator != nil {
		nextCosmosTx = i.cosmosIterator.Tx()
	}

	// If only one type available
	if nextEVMTx == nil {
		return false // Use Cosmos
	}
	if nextCosmosTx == nil {
		return true // Use EVM
	}

	// Both have transactions - compare fees
	cosmosTxFee, ok := nextCosmosTx.(sdk.FeeTx)
	if !ok {
		return true // Use EVM if Cosmos tx has no fee
	}

	cosmosFees := cosmosTxFee.GetFee()
	var cosmosTxEVMDenomFee *sdk.Coin
	for _, coin := range cosmosFees {
		if coin.Denom == i.bondDenom {
			cosmosTxEVMDenomFee = &coin
			break
		}
	}

	if cosmosTxEVMDenomFee == nil {
		return true // Use EVM if Cosmos tx has wrong denomination
	}

	cosmosTxAmount, overflow := uint256.FromBig(cosmosTxEVMDenomFee.Amount.BigInt())
	if overflow || !cosmosTxAmount.Gt(evmFee) {
		return true // Use EVM if Cosmos fee is not higher
	}

	return false // Use Cosmos if it has higher fee
}

func (i *EVMMempoolIterator) Next() mempool.Iterator {
	// Check if both iterators are exhausted
	var hasEVM, hasCosmos bool
	if i.evmIterator != nil {
		nextEVMTx, _ := i.evmIterator.Peek()
		hasEVM = nextEVMTx != nil
	}
	if i.cosmosIterator != nil {
		nextCosmosTx := i.cosmosIterator.Tx()
		hasCosmos = nextCosmosTx != nil
	}

	if !hasEVM && !hasCosmos {
		return nil
	}

	// Advance the iterator that was used
	if i.shouldUseEVM() {
		if i.evmIterator != nil {
			i.evmIterator.Pop()
		}
	} else {
		if i.cosmosIterator != nil {
			i.cosmosIterator = i.cosmosIterator.Next()
		}
	}

	return i
}

// convertEVMToSDKTx converts an EVM transaction to SDK transaction
func (i *EVMMempoolIterator) convertEVMToSDKTx(nextEVMTx *txpool.LazyTransaction) sdk.Tx {
	if nextEVMTx == nil {
		return nil
	}
	msgEthereumTx := &evmtypes.MsgEthereumTx{}
	if err := msgEthereumTx.FromEthereumTx(nextEVMTx.Tx); err != nil {
		return nil // Return nil for invalid tx instead of panicking
	}
	return msgEthereumTx
}

func (i *EVMMempoolIterator) Tx() sdk.Tx {
	var nextEVMTx *txpool.LazyTransaction
	if i.evmIterator != nil {
		nextEVMTx, _ = i.evmIterator.Peek()
	}

	var nextCosmosTx sdk.Tx
	if i.cosmosIterator != nil {
		nextCosmosTx = i.cosmosIterator.Tx()
	}

	// If no transactions available
	if nextEVMTx == nil && nextCosmosTx == nil {
		return nil
	}

	// Return the appropriate transaction
	if i.shouldUseEVM() {
		return i.convertEVMToSDKTx(nextEVMTx)
	} else {
		return nextCosmosTx
	}
}
