package mempool

import (
	"math/big"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	basefactory "github.com/cosmos/evm/testutil/integration/base/factory"
	"github.com/cosmos/evm/testutil/keyring"
	utiltx "github.com/cosmos/evm/testutil/tx"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// TestRemoveCosmosTransaction tests removal of Cosmos SDK transactions
func (s *MempoolIntegrationTestSuite) TestRemoveCosmosTransaction() {
	s.SetupTest()
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

	// Insert transaction
	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	initialCount := mpoolInstance.CountTx()
	s.Require().Greater(initialCount, 0, "mempool should contain the inserted transaction")

	// Remove transaction
	err = mpoolInstance.Remove(tx)
	s.Require().NoError(err)

	finalCount := mpoolInstance.CountTx()
	s.Require().Less(finalCount, initialCount, "mempool count should decrease after removal")

	s.T().Log("Successfully removed Cosmos transaction from mempool")
}

// TestRemoveEVMTransaction tests removal of EVM transactions
func (s *MempoolIntegrationTestSuite) TestRemoveEVMTransaction() {
	s.SetupTest()
	// Create an Ethereum private key and address
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

	// Use factory to create and sign the transaction
	signedMsg, err := s.factory.GenerateSignedMsgEthereumTx(privKey, evmTxArgs)
	s.Require().NoError(err)

	// Use PrepareEthTx to build the transaction properly for EVM messages
	tx, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), privKey, &signedMsg)
	s.Require().NoError(err)

	mpoolInstance := s.network.App.GetMempool()

	// Insert transaction
	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	initialCount := mpoolInstance.CountTx()
	s.Require().Equal(initialCount, 1, "mempool should contain the inserted transaction")

	// Remove transaction
	err = mpoolInstance.Remove(tx)
	s.Require().NoError(err)

	finalCount := mpoolInstance.CountTx()
	s.Require().Equal(finalCount, initialCount-1, "mempool count should decrease after removal")

	s.T().Log("Successfully removed EVM transaction from mempool")
}

// TestRemoveNonExistentTransaction tests removal of transactions not in mempool
func (s *MempoolIntegrationTestSuite) TestRemoveNonExistentTransaction() {
	s.SetupTest()
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Fund the sender
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	// Create a transaction but don't insert it
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

	// Try to remove transaction that was never inserted
	// Note: Remove behavior for non-existent transactions depends on implementation
	// It might return an error or silently succeed
	err = mpoolInstance.Remove(tx)
	// We don't assert on the error since behavior may vary

	finalCount := mpoolInstance.CountTx()
	s.Require().Equal(initialCount, finalCount, "mempool count should remain unchanged")

	s.T().Log("Successfully tested removal of non-existent transaction")
}

// TestRemoveMultipleTransactions tests removal of multiple transactions
func (s *MempoolIntegrationTestSuite) TestRemoveMultipleTransactions() {
	s.SetupTest()
	sender1 := s.keyring.GetKey(0)
	sender2 := s.keyring.GetKey(1)
	recipient := s.keyring.GetKey(2)

	// Fund both senders
	s.FundAccount(sender1.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())
	s.FundAccount(sender2.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	mpoolInstance := s.network.App.GetMempool()

	// Create and insert multiple transactions
	var transactions []sdk.Tx

	for i, sender := range []*keyring.Key{&sender1, &sender2} {
		bankMsg := banktypes.NewMsgSend(
			sender.AccAddr,
			recipient.AccAddr,
			sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(int64(1000+i*100)))),
		)

		tx, err := s.factory.BuildCosmosTx(sender.Priv, basefactory.CosmosTxArgs{
			Msgs: []sdk.Msg{bankMsg},
			Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(int64(1000000000000000+i*100000000000)))),
		})
		s.Require().NoError(err)
		transactions = append(transactions, tx)

		err = mpoolInstance.Insert(s.network.GetContext(), tx)
		s.Require().NoError(err)
	}

	initialCount := mpoolInstance.CountTx()
	s.Require().Equal(initialCount, len(transactions), "mempool should contain all inserted transactions")

	// Remove transactions one by one
	for i, tx := range transactions {
		countBefore := mpoolInstance.CountTx()

		err := mpoolInstance.Remove(tx)
		s.Require().NoError(err)

		countAfter := mpoolInstance.CountTx()
		s.Require().Less(countAfter, countBefore, "mempool count should decrease after removing transaction %d", i)
	}

	finalCount := mpoolInstance.CountTx()
	s.Require().Less(finalCount, initialCount, "final count should be less than initial count")

	s.T().Log("Successfully removed multiple transactions from mempool")
}

// TestRemoveAfterSelect tests removal after selecting transactions
func (s *MempoolIntegrationTestSuite) TestRemoveAfterSelect() {
	s.SetupTest()
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Fund the sender
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	// Create and insert transaction
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

	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	// Select transaction from mempool
	iterator := mpoolInstance.Select(s.network.GetContext(), nil)
	s.Require().NotNil(iterator, "iterator should not be nil")

	selectedTx := iterator.Tx()
	s.Require().NotNil(selectedTx, "should be able to select transaction")

	initialCount := mpoolInstance.CountTx()

	// Remove the selected transaction
	err = mpoolInstance.Remove(selectedTx)
	s.Require().NoError(err)

	finalCount := mpoolInstance.CountTx()
	s.Require().Equal(finalCount, initialCount-1, "mempool count should decrease after removal")

	// Verify transaction is no longer selectable (mempool should be empty)
	newIterator := mpoolInstance.Select(s.network.GetContext(), nil)
	s.Require().Nil(newIterator, "iterator should be nil since mempool is empty after removal")

	s.T().Log("Successfully removed transaction after selection")
}

// TestRemoveMixedTransactionTypes tests removal of both Cosmos and EVM transactions
func (s *MempoolIntegrationTestSuite) TestRemoveMixedTransactionTypes() {
	s.SetupTest()
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Fund the sender
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	mpoolInstance := s.network.App.GetMempool()

	// Create and insert a Cosmos transaction
	bankMsg := banktypes.NewMsgSend(
		sender.AccAddr,
		recipient.AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000))),
	)

	cosmosTx, err := s.factory.BuildCosmosTx(sender.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{bankMsg},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000000000000000))),
	})
	s.Require().NoError(err)

	err = mpoolInstance.Insert(s.network.GetContext(), cosmosTx)
	s.Require().NoError(err)

	// Create an EVM transaction to test mixed removal
	evmSender := s.keyring.GetKey(0)
	privKey := evmSender.Priv

	to := utiltx.GenerateAddress()
	evmTxArgs := evmtypes.EvmTxArgs{
		To:       &to,
		Amount:   big.NewInt(1000),
		GasLimit: 21000,
		GasPrice: big.NewInt(1000000000),
		ChainID:  s.network.GetEIP155ChainID(),
	}

	// Use factory to create and sign the transaction
	signedMsg, err := s.factory.GenerateSignedMsgEthereumTx(privKey, evmTxArgs)
	s.Require().NoError(err)

	// Use PrepareEthTx to build the transaction properly for EVM messages
	evmTx, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), privKey, &signedMsg)
	s.Require().NoError(err)

	err = mpoolInstance.Insert(s.network.GetContext(), evmTx)
	s.Require().NoError(err)

	initialCount := mpoolInstance.CountTx()

	// Remove both transactions
	for i, tx := range []sdk.Tx{cosmosTx, evmTx} {
		countBefore := mpoolInstance.CountTx()

		err := mpoolInstance.Remove(tx)
		s.Require().NoError(err)

		countAfter := mpoolInstance.CountTx()
		s.Require().Less(countAfter, countBefore, "mempool count should decrease after removing transaction %d", i)
	}

	finalCount := mpoolInstance.CountTx()
	s.Require().Less(finalCount, initialCount, "final count should be less than initial count")

	s.T().Log("Successfully removed mixed transaction types from mempool")
}

// TestRemoveAndReinsert tests removing a transaction and then reinserting it
func (s *MempoolIntegrationTestSuite) TestRemoveAndReinsert() {
	s.SetupTest()
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Fund the sender
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	// Create transaction
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

	// Insert transaction
	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	countAfterInsert := mpoolInstance.CountTx()
	s.Require().Equal(countAfterInsert, 1, "mempool should contain transaction after insert")

	// Remove transaction
	err = mpoolInstance.Remove(tx)
	s.Require().NoError(err)

	countAfterRemove := mpoolInstance.CountTx()
	s.Require().Equal(countAfterRemove, 0, "mempool count should decrease after removal")

	// Reinsert the same transaction
	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	countAfterReinsert := mpoolInstance.CountTx()
	s.Require().Equal(countAfterReinsert, 1, "mempool count should increase after reinsertion")

	s.T().Log("Successfully removed and reinserted transaction")
}
