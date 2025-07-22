package mempool

import (
	"context"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"
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
		txDecoder sdk.TxDecoder
	}
	EVMMempoolIterator struct {
		/** Mempool Iterators **/
		evmIterator    *miner.TransactionsByPriceAndNonce
		cosmosIterator mempool.Iterator

		/** Chain Params **/
		bondDenom string
	}
)

func NewEVMMempool(vmKeeper VMKeeperI, txPool *txpool.TxPool, cosmosPool mempool.ExtMempool, txDecoder sdk.TxDecoder) *EVMMempool {
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
	}
}

func (m *EVMMempool) Insert(ctx context.Context, tx sdk.Tx) error {
	// ASSUMPTION: these are all successful upon CheckTx
	/**
	if tx.type == evm {
		insert into legacy pool
	} else {
		insert into cosmos pool
	}
	*/
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return fmt.Errorf("transaction has no messages")
	}

	// Check if this is an EVM transaction
	if len(msgs) == 1 {
		if ethMsg, ok := msgs[0].(*evmtypes.MsgEthereumTx); ok {
			// Insert into EVM pool
			ethTxs := []*ethtypes.Transaction{ethMsg.AsTransaction()}
			errs := m.txPool.Add(ethTxs, true)
			if len(errs) > 0 && errs[0] != nil {
				return errs[0]
			}
			return nil
		}
	}

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
		return fmt.Errorf("expected 1 msg, got %d", len(msgs)) //todo: error type
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
			return fmt.Errorf("expected 1 err, got %d", len(errs))
		}
		return errs[0]
	}
	return nil
}

func (m *EVMMempool) Select(goCtx context.Context, i [][]byte) mempool.Iterator {
	// todo: reuse logic in selectby
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

	combinedIterator := &EVMMempoolIterator{
		evmIterator:    orderedEVMPendingTxes,
		cosmosIterator: cosmosPendingTxes,
		bondDenom:      m.vmKeeper.GetParams(ctx).EvmDenom,
	}

	return combinedIterator
}

func (m *EVMMempool) CountTx() int {
	pending, _ := m.txPool.Stats()
	return m.cosmosPool.CountTx() + pending
}

func (m *EVMMempool) Remove(tx sdk.Tx) error {
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return fmt.Errorf("transaction has no messages")
	}

	// Check if this is an EVM transaction
	if len(msgs) == 1 {
		if ethMsg, ok := msgs[0].(*evmtypes.MsgEthereumTx); ok {
			// Remove from EVM pool
			m.txPool.Subpools[0].RemoveTx(ethMsg.AsTransaction().Hash(), true, true)
			return nil
		}
	}

	// Remove from cosmos pool for non-EVM transactions
	return m.cosmosPool.Remove(tx)
}

func (m *EVMMempool) SelectBy(goCtx context.Context, i [][]byte, f func(sdk.Tx) bool) {
	//todo: reuse logic in select
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

	var combinedIterator mempool.Iterator = &EVMMempoolIterator{
		evmIterator:    orderedEVMPendingTxes,
		cosmosIterator: cosmosPendingTxes,
		bondDenom:      m.vmKeeper.GetParams(ctx).EvmDenom,
	}

	// todo: ensure that this is not an infinite loop
	// both txPool and PriorityNonceMempool should eventually be exhausted
	// should write tests to make sure of this
	for combinedIterator != nil && f(combinedIterator.Tx()) {
		combinedIterator = combinedIterator.Next()
	}
}

func (i *EVMMempoolIterator) Next() mempool.Iterator {
	// Check if iterators are nil
	if i.evmIterator == nil && i.cosmosIterator == nil {
		return nil
	}
	
	var nextEVMTx *txpool.LazyTransaction
	var evmFee *uint256.Int
	if i.evmIterator != nil {
		nextEVMTx, evmFee = i.evmIterator.Peek()
	}
	
	var nextCosmosTx sdk.Tx
	if i.cosmosIterator != nil {
		nextCosmosTx = i.cosmosIterator.Tx()
	}

	// If no EVM transactions, advance cosmos iterator
	if nextEVMTx == nil {
		if nextCosmosTx != nil && i.cosmosIterator != nil {
			i.cosmosIterator = i.cosmosIterator.Next()
			return i
		}
		return nil // Both iterators exhausted
	}

	// If no cosmos transactions, advance EVM iterator
	if nextCosmosTx == nil {
		if i.evmIterator != nil {
			if i.evmIterator != nil {
			i.evmIterator.Pop()
		}
		}
		return i
	}

	// Both have transactions - compare fees
	cosmosTxFee, ok := nextCosmosTx.(sdk.FeeTx)
	if !ok {
		// If cosmos tx doesn't have fees, prioritize EVM
		if i.evmIterator != nil {
			if i.evmIterator != nil {
			i.evmIterator.Pop()
		}
		}
		return i
	}

	cosmosFees := cosmosTxFee.GetFee()

	// We prioritize the bond denom. Everything else gets pushed to lowest priority.
	var cosmosTxEVMDenomFee *sdk.Coin
	for _, coin := range cosmosFees {
		if coin.Denom == i.bondDenom {
			cosmosTxEVMDenomFee = &coin
			break
		}
	}

	if cosmosTxEVMDenomFee == nil {
		// No matching denom, prioritize EVM
		if i.evmIterator != nil {
			i.evmIterator.Pop()
		}
	} else {
		cosmosTxAmount, overflow := uint256.FromBig(cosmosTxEVMDenomFee.Amount.BigInt())
		if overflow {
			// If overflow, prioritize EVM for safety
			if i.evmIterator != nil {
			i.evmIterator.Pop()
		}
		} else if cosmosTxAmount.Gt(evmFee) {
			// Cosmos tx has higher fee
			i.cosmosIterator = i.cosmosIterator.Next()
		} else {
			// EVM tx has higher or equal fee
			if i.evmIterator != nil {
			i.evmIterator.Pop()
		}
		}
	}

	return i
}

func (i *EVMMempoolIterator) Tx() sdk.Tx {
	// Check if iterators are nil
	if i.evmIterator == nil && i.cosmosIterator == nil {
		return nil
	}
	
	var nextEVMTx *txpool.LazyTransaction
	var evmFee *uint256.Int
	if i.evmIterator != nil {
		nextEVMTx, evmFee = i.evmIterator.Peek()
	}
	
	var nextCosmosTx sdk.Tx
	if i.cosmosIterator != nil {
		nextCosmosTx = i.cosmosIterator.Tx()
	}

	// If no EVM transactions, return cosmos transaction
	if nextEVMTx == nil {
		return nextCosmosTx
	}

	// If no cosmos transactions, return EVM transaction
	if nextCosmosTx == nil {
		msgEthereumTx := &evmtypes.MsgEthereumTx{}
		if err := msgEthereumTx.FromEthereumTx(nextEVMTx.Tx); err != nil {
			return nil // Return nil for invalid tx instead of panicking
		}
		return msgEthereumTx
	}

	// Both have transactions - compare fees
	cosmosTxFee, ok := nextCosmosTx.(sdk.FeeTx)
	if !ok {
		// If cosmos tx doesn't have fees, return EVM
		msgEthereumTx := &evmtypes.MsgEthereumTx{}
		if err := msgEthereumTx.FromEthereumTx(nextEVMTx.Tx); err != nil {
			return nil
		}
		return msgEthereumTx
	}

	cosmosFees := cosmosTxFee.GetFee()

	// We prioritize the bond denom. Everything else gets pushed to lowest priority.
	var cosmosTxEVMDenomFee *sdk.Coin
	for _, coin := range cosmosFees {
		if coin.Denom == i.bondDenom {
			cosmosTxEVMDenomFee = &coin
			break
		}
	}

	if cosmosTxEVMDenomFee == nil {
		// No matching denom, return EVM transaction
		msgEthereumTx := &evmtypes.MsgEthereumTx{}
		if err := msgEthereumTx.FromEthereumTx(nextEVMTx.Tx); err != nil {
			return nil
		}
		return msgEthereumTx
	}

	cosmosTxAmount, overflow := uint256.FromBig(cosmosTxEVMDenomFee.Amount.BigInt())
	if overflow {
		// If overflow, return EVM transaction for safety
		msgEthereumTx := &evmtypes.MsgEthereumTx{}
		if err := msgEthereumTx.FromEthereumTx(nextEVMTx.Tx); err != nil {
			return nil
		}
		return msgEthereumTx
	}

	if cosmosTxAmount.Gt(evmFee) {
		return nextCosmosTx
	}

	// EVM tx has higher or equal fee
	msgEthereumTx := &evmtypes.MsgEthereumTx{}
	if err := msgEthereumTx.FromEthereumTx(nextEVMTx.Tx); err != nil {
		return nil
	}
	return msgEthereumTx
}
