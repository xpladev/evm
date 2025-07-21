package mocks

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"
)

type MockCosmosIterator struct {
	txs   []sdk.Tx
	index int
}

func (m *MockCosmosIterator) Next() mempool.Iterator {
	if m.index < len(m.txs)-1 {
		return &MockCosmosIterator{txs: m.txs, index: m.index + 1}
	}
	return nil
}

func (m *MockCosmosIterator) Tx() sdk.Tx {
	if m.index >= len(m.txs) {
		return nil
	}
	return m.txs[m.index]
}
