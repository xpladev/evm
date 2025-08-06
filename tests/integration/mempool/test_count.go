package mempool

import (
	"context"
	"math/big"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	mempool "github.com/cosmos/evm/mempool"
	basefactory "github.com/cosmos/evm/testutil/integration/base/factory"
	utiltx "github.com/cosmos/evm/testutil/tx"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	cosmostx "github.com/cosmos/cosmos-sdk/client/tx"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// TestCountEmptyMempool tests counting transactions in an empty mempool
func (s *MempoolIntegrationTestSuite) TestCountEmptyMempool() {
	mpoolInstance := mempool.GetGlobalEVMMempool()

	// Count transactions in empty mempool
	count := mpoolInstance.CountTx()
	s.Require().GreaterOrEqual(count, 0, "empty mempool should have non-negative count")

	s.T().Log("Successfully counted transactions in empty mempool")
}

// TestCountSingleTransaction tests counting with a single transaction
func (s *MempoolIntegrationTestSuite) TestCountSingleTransaction() {
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Fund the sender
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	mpoolInstance := mempool.GetGlobalEVMMempool()

	// Get initial count
	initialCount := mpoolInstance.CountTx()

	// Create and insert a transaction
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

	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	// Count transactions after insertion
	finalCount := mpoolInstance.CountTx()
	s.Require().Greater(finalCount, initialCount, "count should increase after insertion")

	s.T().Log("Successfully counted single transaction in mempool")
}

// TestCountMultipleTransactions tests counting with multiple transactions with proper sequence numbers
func (s *MempoolIntegrationTestSuite) TestCountMultipleTransactions() {
	sender1 := s.keyring.GetKey(0)
	sender2 := s.keyring.GetKey(1)
	recipient := s.keyring.GetKey(2)

	// Fund both senders
	s.FundAccount(sender1.AccAddr, sdkmath.NewInt(5000000000000000000), s.network.GetBaseDenom())
	s.FundAccount(sender2.AccAddr, sdkmath.NewInt(5000000000000000000), s.network.GetBaseDenom())

	mpoolInstance := mempool.GetGlobalEVMMempool()

	// Get initial count
	initialCount := mpoolInstance.CountTx()

	// Create and insert multiple transactions with incremented sequence numbers
	transactions := []sdk.Tx{}

	// Create 3 transactions from sender1 (sequences 0, 1, 2)
	for seq := 0; seq < 3; seq++ {
		bankMsg := banktypes.NewMsgSend(
			sender1.AccAddr,
			recipient.AccAddr,
			sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(int64(1000+seq*10)))),
		)

		// Build transaction manually without simulation
		tx, err := s.buildTxWithoutSimulation(sender1.Priv, bankMsg, uint64(seq), int64(1000000000000000+seq*10000000000000))
		s.Require().NoError(err)
		transactions = append(transactions, tx)

		err = mpoolInstance.Insert(s.network.GetContext(), tx)
		s.Require().NoError(err)

		// Verify count increases with each insertion
		currentCount := mpoolInstance.CountTx()
		s.T().Logf("After inserting tx %d from sender1 (seq %d): count = %d (initial = %d)", seq+1, seq, currentCount, initialCount)
		s.Require().Greater(currentCount, initialCount, "count should increase with each insertion")
	}

	// Create 3 transactions from sender2 (sequences 0, 1, 2)
	for seq := 0; seq < 3; seq++ {
		bankMsg := banktypes.NewMsgSend(
			sender2.AccAddr,
			recipient.AccAddr,
			sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(int64(2000+seq*10)))),
		)

		// Build transaction manually without simulation
		tx, err := s.buildTxWithoutSimulation(sender2.Priv, bankMsg, uint64(seq), int64(2000000000000000+seq*10000000000000))
		s.Require().NoError(err)
		transactions = append(transactions, tx)

		err = mpoolInstance.Insert(s.network.GetContext(), tx)
		s.Require().NoError(err)

		// Verify count increases with each insertion
		currentCount := mpoolInstance.CountTx()
		s.T().Logf("After inserting tx %d from sender2 (seq %d): count = %d (initial = %d)", seq+1, seq, currentCount, initialCount)
		s.Require().Greater(currentCount, initialCount, "count should increase with each insertion")
	}

	// Final count verification
	finalCount := mpoolInstance.CountTx()
	expectedMinIncrease := len(transactions)
	s.Require().GreaterOrEqual(finalCount-initialCount, expectedMinIncrease, "final count should reflect all inserted transactions")

	s.T().Logf("Successfully counted %d transactions in mempool (increase of %d)", finalCount, finalCount-initialCount)
}

// TestCountMultipleEVMTransactions tests counting with multiple EVM transactions with explicit nonces
func (s *MempoolIntegrationTestSuite) TestCountMultipleEVMTransactions() {
	sender1 := s.keyring.GetKey(0)
	sender2 := s.keyring.GetKey(1)

	mpoolInstance := mempool.GetGlobalEVMMempool()

	// Get initial count
	initialCount := mpoolInstance.CountTx()

	// Create and insert multiple EVM transactions with explicit nonces
	transactions := []sdk.Tx{}
	to := utiltx.GenerateAddress()

	// Create 3 EVM transactions from sender1 (nonces 0, 1, 2)
	for nonce := 0; nonce < 3; nonce++ {
		evmTxArgs := evmtypes.EvmTxArgs{
			Nonce:    uint64(nonce),
			To:       &to,
			Amount:   big.NewInt(int64(1000 + nonce*100)),
			GasLimit: 21000,
			GasPrice: big.NewInt(int64(1000000000 + nonce*100000000)),
			ChainID:  s.network.GetEIP155ChainID(),
		}

		evmMsg, err := s.factory.GenerateSignedMsgEthereumTx(sender1.Priv, evmTxArgs)
		s.Require().NoError(err)

		// Use PrepareEthTx to build the transaction properly for EVM messages
		tx, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), sender1.Priv, &evmMsg)
		s.Require().NoError(err)
		transactions = append(transactions, tx)

		err = mpoolInstance.Insert(s.network.GetContext(), tx)
		s.Require().NoError(err)

		// Verify count increases with each insertion
		currentCount := mpoolInstance.CountTx()
		s.T().Logf("After inserting EVM tx %d from sender1 (nonce %d): count = %d (initial = %d)", nonce+1, nonce, currentCount, initialCount)
		s.Require().Greater(currentCount, initialCount, "count should increase with each insertion")
	}

	// Create 3 EVM transactions from sender2 (nonces 0, 1, 2)
	for nonce := 0; nonce < 3; nonce++ {
		evmTxArgs := evmtypes.EvmTxArgs{
			Nonce:    uint64(nonce),
			To:       &to,
			Amount:   big.NewInt(int64(2000 + nonce*100)),
			GasLimit: 21000,
			GasPrice: big.NewInt(int64(2000000000 + nonce*100000000)),
			ChainID:  s.network.GetEIP155ChainID(),
		}

		evmMsg, err := s.factory.GenerateSignedMsgEthereumTx(sender2.Priv, evmTxArgs)
		s.Require().NoError(err)

		// Use PrepareEthTx to build the transaction properly for EVM messages
		tx, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), sender2.Priv, &evmMsg)
		s.Require().NoError(err)
		transactions = append(transactions, tx)

		err = mpoolInstance.Insert(s.network.GetContext(), tx)
		s.Require().NoError(err)

		// Verify count increases with each insertion
		currentCount := mpoolInstance.CountTx()
		s.T().Logf("After inserting EVM tx %d from sender2 (nonce %d): count = %d (initial = %d)", nonce+1, nonce, currentCount, initialCount)
		s.Require().Greater(currentCount, initialCount, "count should increase with each insertion")
	}

	// Final count verification
	finalCount := mpoolInstance.CountTx()
	expectedMinIncrease := len(transactions)
	s.Require().GreaterOrEqual(finalCount-initialCount, expectedMinIncrease, "final count should reflect all inserted EVM transactions")

	s.T().Logf("Successfully counted %d EVM transactions in mempool (increase of %d)", finalCount, finalCount-initialCount)
}

// TestCountMixedTransactionTypes tests counting with both Cosmos and EVM transactions
func (s *MempoolIntegrationTestSuite) TestCountMixedTransactionTypes() {
	cosmosAccount := s.keyring.GetKey(0)
	evmAccount := s.keyring.GetKey(1)
	recipient := s.keyring.GetKey(2)

	// Fund accounts
	s.FundAccount(cosmosAccount.AccAddr, sdkmath.NewInt(5000000000000000000), s.network.GetBaseDenom())

	mpoolInstance := mempool.GetGlobalEVMMempool()
	initialCount := mpoolInstance.CountTx()

	transactions := []sdk.Tx{}
	to := utiltx.GenerateAddress()

	// Insert alternating Cosmos and EVM transactions
	for i := 0; i < 3; i++ {
		// Insert Cosmos transaction
		bankMsg := banktypes.NewMsgSend(
			cosmosAccount.AccAddr,
			recipient.AccAddr,
			sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(int64(1000+i*10)))),
		)

		cosmosTx, err := s.buildTxWithoutSimulation(cosmosAccount.Priv, bankMsg, uint64(i), int64(1000000000000000+i*10000000000000))
		s.Require().NoError(err)
		transactions = append(transactions, cosmosTx)

		err = mpoolInstance.Insert(s.network.GetContext(), cosmosTx)
		s.Require().NoError(err)

		currentCount := mpoolInstance.CountTx()
		s.T().Logf("After inserting Cosmos tx %d: count = %d (initial = %d)", i+1, currentCount, initialCount)

		// Insert EVM transaction
		evmTxArgs := evmtypes.EvmTxArgs{
			Nonce:    uint64(i),
			To:       &to,
			Amount:   big.NewInt(int64(2000 + i*100)),
			GasLimit: 21000,
			GasPrice: big.NewInt(int64(2000000000 + i*100000000)),
			ChainID:  s.network.GetEIP155ChainID(),
		}

		evmMsg, err := s.factory.GenerateSignedMsgEthereumTx(evmAccount.Priv, evmTxArgs)
		s.Require().NoError(err)

		evmTx, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), evmAccount.Priv, &evmMsg)
		s.Require().NoError(err)
		transactions = append(transactions, evmTx)

		err = mpoolInstance.Insert(s.network.GetContext(), evmTx)
		s.Require().NoError(err)

		currentCount = mpoolInstance.CountTx()
		s.T().Logf("After inserting EVM tx %d: count = %d (initial = %d)", i+1, currentCount, initialCount)
		s.Require().Greater(currentCount, initialCount, "count should increase with each insertion")
	}

	// Final count verification
	finalCount := mpoolInstance.CountTx()
	expectedMinIncrease := len(transactions)
	s.Require().GreaterOrEqual(finalCount-initialCount, expectedMinIncrease, "final count should reflect all mixed transactions")

	s.T().Logf("Successfully counted %d mixed transactions in mempool (increase of %d)", finalCount, finalCount-initialCount)
}

// TestCountEVMTransactions tests counting EVM transactions
func (s *MempoolIntegrationTestSuite) TestCountEVMTransactions() {
	// Use a keyring account that's already funded in genesis
	sender := s.keyring.GetKey(0)
	privKey := sender.Priv

	mpoolInstance := mempool.GetGlobalEVMMempool()
	initialCount := mpoolInstance.CountTx()

	// Create multiple EVM transactions
	to := utiltx.GenerateAddress()
	numEVMTxs := 4

	for i := 0; i < numEVMTxs; i++ {
		evmTxArgs := evmtypes.EvmTxArgs{
			Nonce:    uint64(i),
			To:       &to,
			Amount:   big.NewInt(int64(1000 + i*100)),
			GasLimit: 21000,
			GasPrice: big.NewInt(int64(1000000000 + i*100000000)),
			ChainID:  s.network.GetEIP155ChainID(),
		}

		signedMsg, err := s.factory.GenerateSignedMsgEthereumTx(privKey, evmTxArgs)
		s.Require().NoError(err)

		// Use PrepareEthTx to build the transaction properly for EVM messages
		tx, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), privKey, &signedMsg)
		s.Require().NoError(err)

		err = mpoolInstance.Insert(s.network.GetContext(), tx)
		s.Require().NoError(err)

		// Verify count increases
		currentCount := mpoolInstance.CountTx()
		s.Require().Greater(currentCount, initialCount, "count should increase with EVM transaction insertion")
	}

	finalCount := mpoolInstance.CountTx()
	s.Require().GreaterOrEqual(finalCount-initialCount, numEVMTxs, "final count should reflect all EVM transactions")

	s.T().Logf("Successfully counted %d EVM transactions", finalCount-initialCount)
}

// TestCountMixedTransactions tests counting mixed EVM and Cosmos transactions
func (s *MempoolIntegrationTestSuite) TestCountMixedTransactions() {
	// Setup Cosmos transaction account
	cosmosSender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)
	s.FundAccount(cosmosSender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	// Setup EVM transaction account from keyring
	evmSender := s.keyring.GetKey(1)
	evmPrivKey := evmSender.Priv

	mpoolInstance := mempool.GetGlobalEVMMempool()
	initialCount := mpoolInstance.CountTx()

	// Insert Cosmos transaction
	cosmosMsg := banktypes.NewMsgSend(
		cosmosSender.AccAddr,
		recipient.AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000))),
	)

	cosmosTx, err := s.factory.BuildCosmosTx(cosmosSender.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{cosmosMsg},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000000000000000))),
	})
	s.Require().NoError(err)

	err = mpoolInstance.Insert(s.network.GetContext(), cosmosTx)
	s.Require().NoError(err)

	countAfterCosmos := mpoolInstance.CountTx()
	s.Require().Greater(countAfterCosmos, initialCount, "count should increase after Cosmos transaction")

	// Insert EVM transaction
	to := utiltx.GenerateAddress()
	evmTxArgs := evmtypes.EvmTxArgs{
		Nonce:    0,
		To:       &to,
		Amount:   big.NewInt(2000),
		GasLimit: 21000,
		GasPrice: big.NewInt(2000000000),
		ChainID:  s.network.GetEIP155ChainID(),
	}

	evmMsg, err := s.factory.GenerateSignedMsgEthereumTx(evmPrivKey, evmTxArgs)
	s.Require().NoError(err)

	// Use PrepareEthTx to build the transaction properly for EVM messages
	evmTx, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), evmPrivKey, &evmMsg)
	s.Require().NoError(err)

	err = mpoolInstance.Insert(s.network.GetContext(), evmTx)
	s.Require().NoError(err)

	finalCount := mpoolInstance.CountTx()
	s.Require().Greater(finalCount, countAfterCosmos, "count should increase after EVM transaction")
	s.Require().GreaterOrEqual(finalCount-initialCount, 2, "should have at least 2 new transactions")

	s.T().Logf("Successfully counted mixed transactions: %d total (increase of %d)", finalCount, finalCount-initialCount)
}

// TestCountAfterRemoval tests counting after transaction removal
func (s *MempoolIntegrationTestSuite) TestCountAfterRemoval() {
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Fund the sender
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	mpoolInstance := mempool.GetGlobalEVMMempool()
	initialCount := mpoolInstance.CountTx()

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

	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	countAfterInsert := mpoolInstance.CountTx()
	s.Require().Greater(countAfterInsert, initialCount, "count should increase after insertion")

	// Remove transaction
	err = mpoolInstance.Remove(tx)
	s.Require().NoError(err)

	countAfterRemoval := mpoolInstance.CountTx()
	s.Require().Less(countAfterRemoval, countAfterInsert, "count should decrease after removal")

	s.T().Logf("Count after removal: %d (decreased by %d)", countAfterRemoval, countAfterInsert-countAfterRemoval)
}

// TestCountConsistency tests that count remains consistent across operations
func (s *MempoolIntegrationTestSuite) TestCountConsistency() {
	mpoolInstance := mempool.GetGlobalEVMMempool()

	// Perform multiple count operations and verify consistency
	for i := 0; i < 10; i++ {
		count1 := mpoolInstance.CountTx()
		count2 := mpoolInstance.CountTx()
		s.Require().Equal(count1, count2, "consecutive count calls should return same value")
	}

	// Insert transaction and verify count changes appropriately
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	beforeInsert := mpoolInstance.CountTx()

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

	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	afterInsert := mpoolInstance.CountTx()
	s.Require().Greater(afterInsert, beforeInsert, "count should increase after insertion")

	// Verify count remains consistent after insertion
	for i := 0; i < 5; i++ {
		count := mpoolInstance.CountTx()
		s.Require().Equal(afterInsert, count, "count should remain consistent after insertion")
	}

	s.T().Log("Successfully verified count consistency across operations")
}

// buildTxWithoutSimulation builds a transaction manually without gas simulation
// This allows us to set explicit sequence numbers for testing multiple transactions
func (s *MempoolIntegrationTestSuite) buildTxWithoutSimulation(privKey cryptotypes.PrivKey, msg sdk.Msg, sequence uint64, feeAmount int64) (sdk.Tx, error) {
	txConfig := s.network.App.GetTxConfig()
	txBuilder := txConfig.NewTxBuilder()

	// Set the message
	err := txBuilder.SetMsgs(msg)
	if err != nil {
		return nil, err
	}

	// Set fee amount and gas limit
	fees := sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(feeAmount)))
	txBuilder.SetFeeAmount(fees)
	txBuilder.SetGasLimit(200000) // Fixed gas limit to avoid simulation

	// Get account info for signing
	senderAddress := sdk.AccAddress(privKey.PubKey().Address().Bytes())
	accountKeeper := s.network.App.GetAccountKeeper()
	account := accountKeeper.GetAccount(s.network.GetContext(), senderAddress)

	// Create signer data with explicit sequence
	signerData := authsigning.SignerData{
		ChainID:       s.network.GetChainID(),
		AccountNumber: account.GetAccountNumber(),
		Sequence:      sequence, // Use the explicit sequence provided
		Address:       senderAddress.String(),
		PubKey:        privKey.PubKey(),
	}

	// Sign the transaction
	signMode := signing.SignMode_SIGN_MODE_DIRECT
	sigsV2 := signing.SignatureV2{
		PubKey: privKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  signMode,
			Signature: nil,
		},
		Sequence: sequence,
	}

	err = txBuilder.SetSignatures(sigsV2)
	if err != nil {
		return nil, err
	}

	// Generate signature
	signature, err := cosmostx.SignWithPrivKey(
		context.Background(), signMode, signerData, txBuilder, privKey, txConfig, sequence,
	)
	if err != nil {
		return nil, err
	}

	err = txBuilder.SetSignatures(signature)
	if err != nil {
		return nil, err
	}

	return txBuilder.GetTx(), nil
}
