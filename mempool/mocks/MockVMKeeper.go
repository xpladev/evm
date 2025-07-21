package mocks

import (
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/evm/x/vm/statedb"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"math/big"
)

type MockVMKeeper struct {
	BaseFee  *big.Int
	Params   evmtypes.Params
	Accounts map[common.Address]*statedb.Account
}

func (m *MockVMKeeper) GetBaseFee(_ sdk.Context) *big.Int {
	return m.BaseFee
}

func (m *MockVMKeeper) GetParams(_ sdk.Context) evmtypes.Params {
	return m.Params
}

// Implement statedb.Keeper interface methods
func (m *MockVMKeeper) GetAccount(_ sdk.Context, addr common.Address) *statedb.Account {
	if m.Accounts == nil {
		m.Accounts = make(map[common.Address]*statedb.Account)
	}
	if acc, exists := m.Accounts[addr]; exists {
		return acc
	}
	return nil
}

func (m *MockVMKeeper) GetState(_ sdk.Context, addr common.Address, key common.Hash) common.Hash {
	return common.Hash{}
}

func (m *MockVMKeeper) GetCode(_ sdk.Context, codeHash common.Hash) []byte {
	return nil
}

func (m *MockVMKeeper) GetCodeHash(_ sdk.Context, addr common.Address) common.Hash {
	return common.Hash{}
}

func (m *MockVMKeeper) ForEachStorage(_ sdk.Context, addr common.Address, cb func(key, value common.Hash) bool) {
	// No storage for mock
}

func (m *MockVMKeeper) SetAccount(_ sdk.Context, addr common.Address, account statedb.Account) error {
	if m.Accounts == nil {
		m.Accounts = make(map[common.Address]*statedb.Account)
	}
	m.Accounts[addr] = &account
	return nil
}

func (m *MockVMKeeper) SetState(_ sdk.Context, addr common.Address, key common.Hash, value []byte) {
	// No-op for mock
}

func (m *MockVMKeeper) SetCode(_ sdk.Context, codeHash []byte, code []byte) {
	// No-op for mock
}

func (m *MockVMKeeper) DeleteAccount(_ sdk.Context, addr common.Address) error {
	if m.Accounts != nil {
		delete(m.Accounts, addr)
	}
	return nil
}

func (m *MockVMKeeper) DeleteState(_ sdk.Context, addr common.Address, key common.Hash) {
	// No-op for mock
}

func (m *MockVMKeeper) DeleteCode(_ sdk.Context, codeHash []byte) {
	// No-op for mock
}

func (m *MockVMKeeper) KVStoreKeys() map[string]*storetypes.KVStoreKey {
	return nil
}

// Helper method to add account with balance
func (m *MockVMKeeper) AddAccount(addr common.Address, balance *uint256.Int, nonce uint64) {
	if m.Accounts == nil {
		m.Accounts = make(map[common.Address]*statedb.Account)
	}
	m.Accounts[addr] = &statedb.Account{
		Balance:  balance,
		Nonce:    nonce,
		CodeHash: nil,
	}
}
