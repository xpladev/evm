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
		evmIterator    *miner.TransactionsByPriceAndNonce
		cosmosIterator *mempool.Iterator
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

	combinedIterator := EVMMempoolIterator{
		evmIterator:    orderedEVMPendingTxes,
		cosmosIterator: &cosmosPendingTxes,
	}

	return combinedIterator
}

func (m EVMMempool) CountTx() int {
	//TODO implement me
	panic("implement me")
}

func (m EVMMempool) Remove(tx sdk.Tx) error {
	//TODO implement me
	panic("implement me")
}

func (m EVMMempool) SelectBy(ctx context.Context, i [][]byte, f func(sdk.Tx) bool) {
	// TODO: we need to handle both cosmos and EVM transactions
	// both are ordered by priority nonce
	//TODO implement me
	panic("implement me")
}

func (m EVMMempoolIterator) Next() mempool.Iterator {
	//TODO implement me
	panic("implement me")
}

func (m EVMMempoolIterator) Tx() sdk.Tx {
	//TODO implement me
	panic("implement me")
}
