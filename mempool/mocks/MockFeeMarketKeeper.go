package mocks

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type MockFeeMarketKeeper struct {
	BlockGasWanted uint64
}

func (m *MockFeeMarketKeeper) GetBlockGasWanted(_ sdk.Context) uint64 {
	return m.BlockGasWanted
}