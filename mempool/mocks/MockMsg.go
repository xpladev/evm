package mocks

import sdk "github.com/cosmos/cosmos-sdk/types"

type MockMsg struct{}

func (m *MockMsg) Reset() {}

func (m *MockMsg) String() string {
	return "mock message"
}

func (m *MockMsg) ProtoMessage() {}

func (m *MockMsg) ValidateBasic() error {
	return nil
}

func (m *MockMsg) GetSigners() []sdk.AccAddress {
	return nil
}

func (m *MockMsg) GetSignBytes() []byte {
	return nil
}
