package mempool

import (
	"math/big"

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
	evmtypes "github.com/cosmos/evm/x/vm/types"
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

	// Configure EVM to use the correct chain config
	configurator := evmtypes.NewEVMConfigurator()
	configurator.ResetTestConfig()
	configurator.WithEVMCoinInfo(testconstants.ExampleChainCoinInfo[chainID])
	err := configurator.Configure()
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

// TestEVMTransactionInsertion tests EVM transaction insertion into the mempool.
// This corresponds to the original mock test: TestInsert - EVM transaction success
func (s *MempoolIntegrationTestSuite) TestEVMTransactionInsertion() {
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Fund the sender with EVM tokens
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(1000000000000000000), s.network.GetEVMDenom()) // 1 ETH in wei

	// Get base fee for gas price
	baseFeeResp, err := s.network.GetEvmClient().BaseFee(s.network.GetContext(), &evmtypes.QueryBaseFeeRequest{})
	s.Require().NoError(err)

	// Create and execute EVM transaction
	txRes, err := s.factory.ExecuteEthTx(sender.Priv, evmtypes.EvmTxArgs{
		To:       &recipient.Addr,
		Amount:   big.NewInt(1000),
		GasLimit: 21000,
		GasPrice: baseFeeResp.BaseFee.BigInt(),
		ChainID:  s.network.GetEIP155ChainID(),
	})
	s.Require().NoError(err)
	s.Require().False(txRes.IsErr(), "EVM transaction should succeed")

	// Advance to next block to process transaction
	err = s.network.NextBlock()
	s.Require().NoError(err)

	// Verify recipient received the funds
	recipientBalAfter := s.GetAllBalances(recipient.AccAddr).AmountOf(s.network.GetEVMDenom())
	s.Require().True(recipientBalAfter.GT(sdkmath.ZeroInt()), "recipient should have received funds")
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

	// Test that accounts have initial balances
	bal0 := s.GetAllBalances(key0.AccAddr)
	s.Require().False(bal0.IsZero(), "key 0 should have positive balance")

	s.T().Logf("Test setup successful - accounts funded and network ready")
}

// TestCosmosTransactionInsertion tests Cosmos transaction insertion into the mempool.
// This corresponds to the original mock test: TestInsert - cosmos transaction success
func (s *MempoolIntegrationTestSuite) TestCosmosTransactionInsertion() {
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Fund the sender
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom()) // Much more funding

	initialSenderBal := s.GetAllBalances(sender.AccAddr).AmountOf(s.network.GetBaseDenom())
	initialRecipientBal := s.GetAllBalances(recipient.AccAddr).AmountOf(s.network.GetBaseDenom())

	// Create bank send message
	sendAmount := sdkmath.NewInt(1000)
	bankMsg := banktypes.NewMsgSend(
		sender.AccAddr,
		recipient.AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sendAmount)),
	)

	// Build and broadcast transaction
	txRes, err := s.factory.ExecuteCosmosTx(sender.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{bankMsg},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000000000000000000))), // Much higher fee
	})
	s.Require().NoError(err)
	if txRes.IsErr() {
		s.T().Logf("Transaction error: %v", txRes.String())
	}
	s.Require().False(txRes.IsErr(), "Cosmos transaction should succeed")

	// Advance to next block to process transaction
	err = s.network.NextBlock()
	s.Require().NoError(err)

	// Verify balances
	finalSenderBal := s.GetAllBalances(sender.AccAddr).AmountOf(s.network.GetBaseDenom())
	finalRecipientBal := s.GetAllBalances(recipient.AccAddr).AmountOf(s.network.GetBaseDenom())

	// Verify sender balance decreased (sent amount + some fee)
	s.Require().True(finalSenderBal.LT(initialSenderBal), "sender balance should have decreased")

	// Verify recipient balance increased by exactly the send amount
	expectedRecipientBal := initialRecipientBal.Add(sendAmount)
	s.Require().Equal(expectedRecipientBal, finalRecipientBal, "recipient balance should increase by send amount")

	s.T().Logf("Transaction succeeded - sender balance: %s -> %s, recipient balance: %s -> %s",
		initialSenderBal, finalSenderBal, initialRecipientBal, finalRecipientBal)
}

// TestEmptyTransactionRejection tests that transactions with no messages are rejected.
// This corresponds to the original mock test: TestInsert - empty transaction should fail
func (s *MempoolIntegrationTestSuite) TestEmptyTransactionRejection() {
	// Create a transaction with no messages
	txRes, err := s.factory.ExecuteCosmosTx(s.keyring.GetKey(0).Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{}, // Empty messages
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000))),
	})

	// The transaction should fail during execution
	s.Require().Error(err)
	if err == nil {
		s.Require().True(txRes.IsErr(), "transaction with no messages should fail")
	}
}
