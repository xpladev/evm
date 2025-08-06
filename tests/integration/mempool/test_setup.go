package mempool

import (
	"github.com/stretchr/testify/suite"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"

	testconstants "github.com/cosmos/evm/testutil/constants"
	basefactory "github.com/cosmos/evm/testutil/integration/base/factory"
	"github.com/cosmos/evm/testutil/integration/evm/factory"
	"github.com/cosmos/evm/testutil/integration/evm/grpc"
	"github.com/cosmos/evm/testutil/integration/evm/network"
	"github.com/cosmos/evm/testutil/keyring"
)

// MempoolIntegrationTestSuite is the base test suite for mempool integration tests.
// It provides the infrastructure to test mempool behavior without mocks.
type MempoolIntegrationTestSuite struct {
	suite.Suite

	create  network.CreateEvmApp
	options []network.ConfigOption
	network *network.UnitTestNetwork
	factory factory.TxFactory
	keyring keyring.Keyring
}

// NewMempoolIntegrationTestSuite creates a new instance of the test suite.
func NewMempoolIntegrationTestSuite(create network.CreateEvmApp, options ...network.ConfigOption) *MempoolIntegrationTestSuite {
	return &MempoolIntegrationTestSuite{
		create:  create,
		options: options,
	}
}

// SetupTest initializes the test environment with default settings.
func (s *MempoolIntegrationTestSuite) SetupTest() {
	s.SetupTestWithChainID(testconstants.ExampleChainID)
}

// SetupTestWithChainID initializes the test environment with a specific chain ID.
func (s *MempoolIntegrationTestSuite) SetupTestWithChainID(chainID testconstants.ChainID) {
	s.keyring = keyring.New(3)

	options := []network.ConfigOption{
		network.WithChainID(chainID),
		network.WithPreFundedAccounts(s.keyring.GetAllAccAddrs()...),
	}
	options = append(options, s.options...)

	nw := network.NewUnitTestNetwork(s.create, options...)
	gh := grpc.NewIntegrationHandler(nw)
	tf := factory.New(nw, gh)

	err := nw.NextBlock()
	s.Require().NoError(err)

	s.network = nw
	s.factory = tf
}

// FundAccount funds an account with a specific amount of a given denomination.
func (s *MempoolIntegrationTestSuite) FundAccount(addr sdk.AccAddress, amount sdkmath.Int, denom string) {
	coins := sdk.NewCoins(sdk.NewCoin(denom, amount))

	// Use the bank keeper to mint and send coins to the account
	err := s.network.App.GetBankKeeper().MintCoins(s.network.GetContext(), minttypes.ModuleName, coins)
	s.Require().NoError(err)

	err = s.network.App.GetBankKeeper().SendCoinsFromModuleToAccount(s.network.GetContext(), minttypes.ModuleName, addr, coins)
	s.Require().NoError(err)
}

// GetAllBalances returns all balances for the given account address.
func (s *MempoolIntegrationTestSuite) GetAllBalances(addr sdk.AccAddress) sdk.Coins {
	return s.network.App.GetBankKeeper().GetAllBalances(s.network.GetContext(), addr)
}

// TestBasicSetup tests that the test environment is properly set up
func (s *MempoolIntegrationTestSuite) TestBasicSetup() {
	// Test that network and keyring are initialized
	s.Require().NotNil(s.network, "network should be initialized")
	s.Require().NotNil(s.keyring, "keyring should be initialized")
	s.Require().NotNil(s.factory, "factory should be initialized")

	// Test that we can get keys
	key0 := s.keyring.GetKey(0)
	key1 := s.keyring.GetKey(1)
	s.Require().NotNil(key0, "key 0 should exist")
	s.Require().NotNil(key1, "key 1 should exist")

	s.Require().Equal(s.network.GetContext().BlockHeight(), int64(2))

	// Test that accounts have initial balances
	bal0 := s.GetAllBalances(key0.AccAddr)
	s.Require().False(bal0.IsZero(), "key 0 should have positive balance")

	s.T().Logf("Test setup successful - accounts funded and network ready")
}

// TestMempoolCount verifies transaction counting functionality
func (s *MempoolIntegrationTestSuite) TestMempoolCount() {
	sender := s.keyring.GetKey(0)

	// Fund the sender
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	// Create and broadcast a transaction (this will add it to mempool and then process it)
	bankMsg := banktypes.NewMsgSend(
		sender.AccAddr,
		s.keyring.GetKey(1).AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000))),
	)

	txRes, err := s.factory.ExecuteCosmosTx(sender.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{bankMsg},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000000000000000))),
	})
	s.Require().NoError(err)
	s.Require().False(txRes.IsErr())

	// Process the transaction
	err = s.network.NextBlock()
	s.Require().NoError(err)

	s.T().Log("Mempool count test completed successfully")
}
