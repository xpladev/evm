package mocks

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	protov2 "google.golang.org/protobuf/proto"
)

type MockFeeTx struct {
	Fees sdk.Coins
	Msgs []sdk.Msg
}

func (m *MockFeeTx) GetFee() sdk.Coins {
	return m.Fees
}

func (m *MockFeeTx) GetMsgs() []sdk.Msg {
	return m.Msgs
}

func (m *MockFeeTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}

func (m *MockFeeTx) ValidateBasic() error {
	return nil
}
