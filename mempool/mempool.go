package mempool

import (
	"context"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"
	vmkeeper "github.com/cosmos/evm/x/vm/keeper"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/txpool/legacypool"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/holiman/uint256"
)

var _ mempool.ExtMempool = EVMMempool{}
var _ mempool.Iterator = EVMMempoolIterator{}

type (
	EVMMempool struct {
		/** Keepers **/
		vmKeeper vmkeeper.Keeper

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

func NewEVMMempool(vmKeeper vmkeeper.Keeper, txPool *txpool.TxPool, legacyPool *legacypool.LegacyPool, cosmosPool mempool.ExtMempool, txDecoder sdk.TxDecoder) *EVMMempool {
	return &EVMMempool{
		vmKeeper:     vmKeeper,
		txPool:       txPool,
		legacyTxPool: legacyPool,
		cosmosPool:   cosmosPool,
		txDecoder:    txDecoder,
	}
}

func (m EVMMempool) Insert(ctx context.Context, tx sdk.Tx) error {
	// ASSUMPTION: these are all successful upon CheckTx
	/**
	if tx.type == evm {
		insert into legacy pool
	} else {
		insert into cosmos pool
	}
	*/
	var ethTxs []*ethtypes.Transaction
	for _, msg := range tx.GetMsgs() {
		ethMsg, ok := msg.(*evmtypes.MsgEthereumTx)
		if ok {
			msgs := tx.GetMsgs()
			if len(msgs) != 1 {
				return fmt.Errorf("expected 1 MsgEthereumTx, got %d", len(msgs)) //todo error type
			}
			ethTxs = append(ethTxs, ethMsg.AsTransaction())
			continue
		} else {
			err := m.cosmosPool.Insert(ctx, tx)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (m EVMMempool) InsertInvalidSequence(txBytes []byte) error {
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

func (m EVMMempool) Select(goCtx context.Context, i [][]byte) mempool.Iterator {
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

func (m EVMMempool) CountTx() int {
	pending, _ := m.txPool.Stats()
	return m.cosmosPool.CountTx() + pending
}

func (m EVMMempool) Remove(tx sdk.Tx) error {
	//TODO implement me
	panic("implement me")
}

func (m EVMMempool) SelectBy(goCtx context.Context, i [][]byte, f func(sdk.Tx) bool) {
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

	for combinedIterator != nil && f(combinedIterator.Tx()) {
		combinedIterator = combinedIterator.Next()
	}
}

func (i *EVMMempoolIterator) Next() mempool.Iterator {
	nextEVMTx, evmFee := i.evmIterator.Peek()
	if nextEVMTx == nil {
		i.cosmosIterator.Next()
	}

	nextCosmosTx, ok := i.cosmosIterator.Tx().(sdk.FeeTx)
	if !ok {
		panic("expected fee Tx") // not supporting ambiguous priorities, since evm is based on fees
	}
	if nextCosmosTx == nil {
		i.evmIterator.Pop()
	}

	cosmosFees := nextCosmosTx.GetFee()

	// We prioritize the bond denom. Everything else gets pushed to lowest priority.
	// Comparing fees for two different tokens is subjective and would require custom
	var cosmosTxEVMDenomFee *sdk.Coin
	for _, coin := range cosmosFees {
		if coin.Denom == i.bondDenom {
			cosmosTxEVMDenomFee = &coin
		}
	}
	if cosmosTxEVMDenomFee == nil {
		i.evmIterator.Pop()
	} else {
		cosmosTxAmount, overflow := uint256.FromBig(cosmosTxEVMDenomFee.Amount.BigInt())
		if overflow {
			panic("conversion error: overflow")
		}
		if cosmosTxAmount.Gt(evmFee) {
			i.cosmosIterator.Next()
		} else {
			i.evmIterator.Pop()
		}
	}

	return i
}

func (i *EVMMempoolIterator) Tx() sdk.Tx {
	nextEVMTx, evmFee := i.evmIterator.Peek()
	msgEthereumTx := &evmtypes.MsgEthereumTx{}
	if err := msgEthereumTx.FromEthereumTx(nextEVMTx.Tx); err != nil {
		panic("invalid tx")
	}
	nextCosmosTx, ok := i.cosmosIterator.Tx().(sdk.FeeTx)
	if !ok {
		panic("expected fee Tx") // not supporting ambiguous priorities, since evm is based on fees
	}
	cosmosFees := nextCosmosTx.GetFee()

	// We prioritize the bond denom. Everything else gets pushed to lowest priority.
	// Comparing fees for two different tokens is subjective and would require custom
	var cosmosTxEVMDenomFee *sdk.Coin
	for _, coin := range cosmosFees {
		if coin.Denom == i.bondDenom {
			cosmosTxEVMDenomFee = &coin
		}
	}
	if cosmosTxEVMDenomFee == nil {
		return msgEthereumTx
	} else {
		cosmosTxAmount, overflow := uint256.FromBig(cosmosTxEVMDenomFee.Amount.BigInt())
		if overflow {
			panic("conversion error: overflow")
		}
		if cosmosTxAmount.Gt(evmFee) {
			return nextCosmosTx
		} else {
			return msgEthereumTx
		}
	}

	return nil
}
