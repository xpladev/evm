package mocks

import "github.com/ethereum/go-ethereum/common"

// MockReserver implements the txpool.Reserver interface for testing
type MockReserver struct{}

func (m *MockReserver) Hold(_ common.Address) error {
	return nil
}

func (m *MockReserver) Release(_ common.Address) error {
	return nil
}

func (m *MockReserver) Has(_ common.Address) bool {
	return false
}
