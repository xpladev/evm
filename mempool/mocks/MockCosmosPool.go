package mocks

import (
	"context"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"
	"sort"
)

type MockCosmosPool struct {
	Txs       []sdk.Tx
	insertErr error
	removeErr error
}

func (m *MockCosmosPool) Insert(_ context.Context, tx sdk.Tx) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.Txs = append(m.Txs, tx)
	return nil
}

func (m *MockCosmosPool) Select(_ context.Context, _ [][]byte) mempool.Iterator {
	// Sort transactions by fee in descending order (highest first)
	sortedTxs := make([]sdk.Tx, len(m.Txs))
	copy(sortedTxs, m.Txs)

	// Simple sorting by fee amount for the "uatom" denom
	sort.Slice(sortedTxs, func(i, j int) bool {
		feeI := getFeeTxAmount(sortedTxs[i])
		feeJ := getFeeTxAmount(sortedTxs[j])
		return feeI > feeJ
	})

	return &MockCosmosIterator{txs: sortedTxs, index: 0}
}

// Helper function to get fee amount from a transaction
func getFeeTxAmount(tx sdk.Tx) int64 {
	if feeTx, ok := tx.(sdk.FeeTx); ok {
		fees := feeTx.GetFee()
		for _, coin := range fees {
			if coin.Denom == "uatom" {
				return coin.Amount.Int64()
			}
		}
	}
	return 0
}

func (m *MockCosmosPool) CountTx() int {
	return len(m.Txs)
}

func (m *MockCosmosPool) Remove(tx sdk.Tx) error {
	if m.removeErr != nil {
		return m.removeErr
	}
	for i, storedTx := range m.Txs {
		if storedTx == tx {
			m.Txs = append(m.Txs[:i], m.Txs[i+1:]...)
			break
		}
	}
	return nil
}

func (m *MockCosmosPool) SelectBy(_ context.Context, _ [][]byte, _ func(sdk.Tx) bool) {}
