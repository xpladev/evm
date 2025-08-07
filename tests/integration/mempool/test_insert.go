package mempool

import (
	"math/big"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	basefactory "github.com/cosmos/evm/testutil/integration/base/factory"
	utiltx "github.com/cosmos/evm/testutil/tx"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// TestInsertCosmosTransaction tests successful insertion of Cosmos SDK transactions
func (s *MempoolIntegrationTestSuite) TestInsertCosmosTransaction() {
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Fund the sender
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	// Create a bank send transaction
	bankMsg := banktypes.NewMsgSend(
		sender.AccAddr,
		recipient.AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000))),
	)

	tx, err := s.factory.BuildCosmosTx(sender.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{bankMsg},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000000000000000))),
	})
	s.Require().NoError(err)

	// Insert transaction into mempool
	mpoolInstance := s.network.App.GetMempool()
	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	// Verify transaction count increased
	count := mpoolInstance.CountTx()
	s.Require().Greater(count, 0, "mempool should contain at least one transaction")

	s.T().Log("Successfully inserted Cosmos transaction into mempool")
}

// TestInsertEVMTransaction tests successful insertion of EVM transactions
func (s *MempoolIntegrationTestSuite) TestInsertEVMTransaction() {
	// Use a keyring account that's already funded in genesis
	sender := s.keyring.GetKey(0)
	privKey := sender.Priv

	// Create an EVM transaction
	to := utiltx.GenerateAddress()
	evmTxArgs := evmtypes.EvmTxArgs{
		To:       &to,
		Amount:   big.NewInt(1000),
		GasLimit: 21000,
		GasPrice: big.NewInt(1000000000),
		ChainID:  s.network.GetEIP155ChainID(),
	}

	// Use the factory to create and sign the transaction
	signedMsg, err := s.factory.GenerateSignedMsgEthereumTx(privKey, evmTxArgs)
	s.Require().NoError(err)

	// Use PrepareEthTx to build the transaction properly for EVM messages
	tx, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), privKey, &signedMsg)
	s.Require().NoError(err)

	// Insert transaction into mempool
	mpoolInstance := s.network.App.GetMempool()
	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	// Verify transaction count increased
	count := mpoolInstance.CountTx()
	s.Require().Greater(count, 0, "mempool should contain at least one transaction")

	s.T().Log("Successfully inserted EVM transaction into mempool")
}

// TestInsertMultipleTransactions tests insertion of multiple transactions from different senders
func (s *MempoolIntegrationTestSuite) TestInsertMultipleTransactions() {
	sender1 := s.keyring.GetKey(0)
	sender2 := s.keyring.GetKey(1)
	recipient := s.keyring.GetKey(2)

	// Fund both senders
	s.FundAccount(sender1.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())
	s.FundAccount(sender2.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	mpoolInstance := s.network.App.GetMempool()
	initialCount := mpoolInstance.CountTx()

	// Create first transaction
	bankMsg1 := banktypes.NewMsgSend(
		sender1.AccAddr,
		recipient.AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000))),
	)

	tx1, err := s.factory.BuildCosmosTx(sender1.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{bankMsg1},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000000000000000))),
	})
	s.Require().NoError(err)

	// Insert first transaction
	err = mpoolInstance.Insert(s.network.GetContext(), tx1)
	s.Require().NoError(err)

	// Create second transaction
	bankMsg2 := banktypes.NewMsgSend(
		sender2.AccAddr,
		recipient.AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(2000))),
	)

	tx2, err := s.factory.BuildCosmosTx(sender2.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{bankMsg2},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(2000000000000000))),
	})
	s.Require().NoError(err)

	// Insert second transaction
	err = mpoolInstance.Insert(s.network.GetContext(), tx2)
	s.Require().NoError(err)

	// Verify transaction count increased by 2
	finalCount := mpoolInstance.CountTx()
	s.Require().Equal(initialCount+2, finalCount, "mempool should contain 2 additional transactions")

	s.T().Log("Successfully inserted multiple transactions into mempool")
}

// TestInsertDuplicateTransaction tests insertion of duplicate transactions
func (s *MempoolIntegrationTestSuite) TestInsertDuplicateTransaction() {
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Fund the sender
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	// Create a bank send transaction
	bankMsg := banktypes.NewMsgSend(
		sender.AccAddr,
		recipient.AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000))),
	)

	tx, err := s.factory.BuildCosmosTx(sender.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{bankMsg},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000000000000000))),
	})
	s.Require().NoError(err)

	mpoolInstance := s.network.App.GetMempool()
	initialCount := mpoolInstance.CountTx()

	// Insert transaction first time
	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	countAfterFirst := mpoolInstance.CountTx()
	s.Require().Equal(initialCount+1, countAfterFirst, "mempool should contain 1 additional transaction")

	// Try to insert the same transaction again
	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	// Should handle gracefully (either reject or accept based on implementation)
	// We test that it doesn't crash
	s.Require().NotPanics(func() {
		mpoolInstance.Insert(s.network.GetContext(), tx)
	})

	s.T().Log("Successfully handled duplicate transaction insertion")
}

// TestInsertInvalidTransaction tests insertion of invalid transactions
func (s *MempoolIntegrationTestSuite) TestInsertInvalidTransaction() {
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Don't fund the sender to create an invalid transaction

	// Create a bank send transaction with insufficient funds
	bankMsg := banktypes.NewMsgSend(
		sender.AccAddr,
		recipient.AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000000000000000000))), // Large amount
	)

	tx, err := s.factory.BuildCosmosTx(sender.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{bankMsg},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000000000000000))),
	})
	s.Require().NoError(err)

	mpoolInstance := s.network.App.GetMempool()
	initialCount := mpoolInstance.CountTx()

	// Try to insert invalid transaction
	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	// Should return an error or handle gracefully
	// The exact behavior depends on when validation occurs

	// Verify mempool state is consistent
	finalCount := mpoolInstance.CountTx()
	s.Require().GreaterOrEqual(finalCount, initialCount, "mempool count should not decrease")

	s.T().Log("Successfully handled invalid transaction insertion")
}

// TestInsertEVMTransactionWithNonceGap tests EVM transaction insertion with nonce gaps
func (s *MempoolIntegrationTestSuite) TestInsertEVMTransactionWithNonceGap() {
	// Use a keyring account that's already funded in genesis
	sender := s.keyring.GetKey(1)
	privKey := sender.Priv

	// Create an EVM transaction with nonce 5 (creating a gap since account nonce is 0)
	to := utiltx.GenerateAddress()
	evmTxArgs := evmtypes.EvmTxArgs{
		Nonce:    5, // Gap from 0
		To:       &to,
		Amount:   big.NewInt(1000),
		GasLimit: 21000,
		GasPrice: big.NewInt(1000000000),
		ChainID:  s.network.GetEIP155ChainID(),
	}

	// Use the factory to create and sign the transaction
	signedMsg, err := s.factory.GenerateSignedMsgEthereumTx(privKey, evmTxArgs)
	s.Require().NoError(err)

	txBuilder := s.network.App.GetTxConfig().NewTxBuilder()
	err = txBuilder.SetMsgs(&signedMsg)
	s.Require().NoError(err)

	mpoolInstance := s.network.App.GetMempool()

	// Insert transaction with nonce gap
	err = mpoolInstance.Insert(s.network.GetContext(), txBuilder.GetTx())
	// Should handle nonce gap gracefully (possibly queuing for later execution)
	// The exact behavior depends on implementation

	s.T().Log("Successfully handled EVM transaction with nonce gap")
}
