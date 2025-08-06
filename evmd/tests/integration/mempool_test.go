package integration

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/cosmos/evm/tests/integration/mempool"
)

func TestMempoolIntegrationTestSuite(t *testing.T) {
	suite.Run(t, mempool.NewMempoolIntegrationTestSuite(CreateEvmd))
}
