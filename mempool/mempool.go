package mempool

import (
	"context"
	"cosmossdk.io/math"
	"errors"
	"fmt"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"
	mempool2 "github.com/cosmos/cosmos-sdk/types/mempool"
	"github.com/cosmos/evm/mempool/miner"
	"github.com/cosmos/evm/mempool/txpool"
	"github.com/cosmos/evm/mempool/txpool/legacypool"
	"github.com/cosmos/evm/x/precisebank/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"sync"
)

var _ mempool.ExtMempool = &EVMMempool{}

type (
	EVMMempool struct {
		/** Keepers **/
		vmKeeper VMKeeperI

		/** Mempools **/
		txPool       *txpool.TxPool
		legacyTxPool *legacypool.LegacyPool
		cosmosPool   mempool.ExtMempool

		/** Utils **/
		txConfig   client.TxConfig
		blockchain *Blockchain
		bondDenom  string
		evmDenom   string

		/** Concurrency **/
		mtx sync.Mutex
	}
)

type EVMMempoolConfig struct {
	TxPool     *txpool.TxPool
	CosmosPool mempool.ExtMempool
}

func NewEVMMempool(ctx func(height int64, prove bool) (sdk.Context, error), vmKeeper VMKeeperI, feeMarketKeeper FeeMarketKeeperI, txConfig client.TxConfig, config *EVMMempoolConfig) *EVMMempool {
	var txPool *txpool.TxPool
	var cosmosPool mempool.ExtMempool

	bondDenom := evmtypes.GetEVMCoinDenom()
	evmDenom := types.ExtendedCoinDenom()

	if config != nil {
		txPool = config.TxPool
		cosmosPool = config.CosmosPool
	}

	var blockchain *Blockchain
	if txPool == nil {
		blockchain = NewBlockchain(ctx, vmKeeper, feeMarketKeeper)
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
		txConfig:     txConfig,
		blockchain:   blockchain,
		bondDenom:    bondDenom,
		evmDenom:     evmDenom,
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
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// ASSUMPTION: these are all successful upon CheckTx
	// todo: should not allow insertion before block 1 has completed
	// Try to get EVM message
	ethMsg, err := m.getEVMMessage(tx)
	if err == nil {
		// Insert into EVM pool
		ethTxs := []*ethtypes.Transaction{ethMsg.AsTransaction()}
		fmt.Println("Inserting eth tx:", ethTxs)
		errs := m.txPool.Add(ethTxs, true)
		if len(errs) > 0 && errs[0] != nil {
			return errs[0]
		}
		return nil
	}

	// Insert into cosmos pool for non-EVM transactions
	fmt.Println("Inserting Cosmos tx:", tx)
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
	tx, err := m.txConfig.TxDecoder()(txBytes)
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
	fmt.Println("Inserting eth tx:", ethTxs[0].Hash())
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
func (m *EVMMempool) setupPendingTransactions(goCtx context.Context, i [][]byte) (*miner.TransactionsByPriceAndNonce, mempool2.Iterator) {
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

	return orderedEVMPendingTxes, cosmosPendingTxes
}

func (m *EVMMempool) Select(goCtx context.Context, i [][]byte) mempool.Iterator {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	evmIterator, cosmosIterator := m.setupPendingTransactions(goCtx, i)

	combinedIterator := NewEVMMempoolIterator(evmIterator, cosmosIterator, m.txConfig, m.bondDenom, m.blockchain.Config().ChainID)

	return combinedIterator
}

func (m *EVMMempool) CountTx() int {
	pending, _ := m.txPool.Stats()
	return m.cosmosPool.CountTx() + pending
}

func (m *EVMMempool) Remove(tx sdk.Tx) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// Try to get EVM message
	ethMsg, err := m.getEVMMessage(tx)
	if err == nil {
		// Remove from EVM pool
		fmt.Println("Removing eth tx:", ethMsg.AsTransaction().Hash())
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
	m.mtx.Lock()
	defer m.mtx.Unlock()

	evmIterator, cosmosIterator := m.setupPendingTransactions(goCtx, i)

	var combinedIterator = NewEVMMempoolIterator(evmIterator, cosmosIterator, m.txConfig, m.bondDenom, m.blockchain.Config().ChainID)

	for combinedIterator != nil && f(combinedIterator.Tx()) {
		combinedIterator = combinedIterator.Next()
	}
}
