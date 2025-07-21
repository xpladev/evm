package mocks

import (
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/evm/x/vm/statedb"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"math/big"
	"sync/atomic"
)

// MockBlockChain implements the BlockChain interface required by legacypool
type MockBlockChain struct {
	config   *params.ChainConfig
	gasLimit atomic.Uint64
	statedb  vm.StateDB
	keeper   *MockVMKeeper
}

func NewMockBlockChain(keeper *MockVMKeeper) *MockBlockChain {
	// Use test chain config with EIP-1559 enabled
	config := *params.TestChainConfig
	config.BerlinBlock = common.Big0
	config.LondonBlock = common.Big0

	bc := &MockBlockChain{
		config: &config,
		keeper: keeper,
	}
	bc.gasLimit.Store(10000000)

	// Create a StateDB instance for this blockchain
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, nil)
	bc.statedb = statedb.New(ctx, keeper, statedb.NewEmptyTxConfig(common.Hash{}))
	return bc
}

// GetStateDB creates a new StateDB instance for blockchain operations
func (m *MockBlockChain) GetStateDB(ctx sdk.Context) vm.StateDB {
	return statedb.New(ctx, m.keeper, statedb.NewEmptyTxConfig(common.Hash{}))
}

func (m *MockBlockChain) Config() *params.ChainConfig {
	return m.config
}

func (m *MockBlockChain) CurrentBlock() *ethtypes.Header {
	return &ethtypes.Header{
		Number:     big.NewInt(1),
		Difficulty: common.Big0,
		GasLimit:   m.gasLimit.Load(),
		Time:       0,
	}
}

func (m *MockBlockChain) GetBlock(_ common.Hash, _ uint64) *ethtypes.Block {
	return ethtypes.NewBlock(m.CurrentBlock(), nil, nil, trie.NewStackTrie(nil))
}

func (m *MockBlockChain) StateAt(_ common.Hash) (vm.StateDB, error) {
	return m.statedb, nil
}
