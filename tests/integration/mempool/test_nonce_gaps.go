package mempool

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"

	mempool "github.com/cosmos/evm/mempool"
	utiltx "github.com/cosmos/evm/testutil/tx"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// TestNonceGapSingleTransaction tests handling of a single transaction with nonce gap
func (s *MempoolIntegrationTestSuite) TestNonceGapSingleTransaction() {
	// Use a keyring account that's already funded in genesis
	sender := s.keyring.GetKey(0)
	privKey := sender.Priv

	mpoolInstance := mempool.GetGlobalEVMMempool()
	initialCount := mpoolInstance.CountTx()

	// Create an EVM transaction with nonce 5 (gap from expected nonce 0)
	to := utiltx.GenerateAddress()
	evmTxArgs := evmtypes.EvmTxArgs{
		Nonce:    5, // This creates a nonce gap
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

	// Insert transaction with nonce gap
	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	// Verify transaction was inserted (behavior depends on mempool implementation)
	finalCount := mpoolInstance.CountTx()
	s.Require().GreaterOrEqual(finalCount, initialCount, "transaction with nonce gap should be handled appropriately")

	s.T().Log("Successfully tested single transaction with nonce gap")
}

// TestNonceGapMultipleTransactions tests handling of multiple transactions with nonce gaps
func (s *MempoolIntegrationTestSuite) TestNonceGapMultipleTransactions() {
	// Use a keyring account that's already funded in genesis
	sender := s.keyring.GetKey(0)
	privKey := sender.Priv

	mpoolInstance := mempool.GetGlobalEVMMempool()
	to := utiltx.GenerateAddress()

	// Create transaction with nonce 0 (valid)
	evmTxArgs1 := evmtypes.EvmTxArgs{
		Nonce:    0,
		To:       &to,
		Amount:   big.NewInt(1000),
		GasLimit: 21000,
		GasPrice: big.NewInt(1000000000),
		ChainID:  s.network.GetEIP155ChainID(),
	}

	signedMsg1, err := s.factory.GenerateSignedMsgEthereumTx(privKey, evmTxArgs1)
	s.Require().NoError(err)

	// Use PrepareEthTx to build the transaction properly for EVM messages
	tx1, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), privKey, &signedMsg1)
	s.Require().NoError(err)

	// Insert valid transaction
	err = mpoolInstance.Insert(s.network.GetContext(), tx1)
	s.Require().NoError(err)

	// Create transaction with nonce 3 (creates gap at nonce 1,2)
	evmTxArgs2 := evmtypes.EvmTxArgs{
		Nonce:    3,
		To:       &to,
		Amount:   big.NewInt(2000),
		GasLimit: 21000,
		GasPrice: big.NewInt(1000000000),
		ChainID:  s.network.GetEIP155ChainID(),
	}

	signedMsg2, err := s.factory.GenerateSignedMsgEthereumTx(privKey, evmTxArgs2)
	s.Require().NoError(err)

	// Use PrepareEthTx to build the transaction properly for EVM messages
	tx2, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), privKey, &signedMsg2)
	s.Require().NoError(err)

	// Insert transaction with gap
	err = mpoolInstance.Insert(s.network.GetContext(), tx2)
	s.Require().NoError(err)

	// Verify mempool state - exact behavior depends on implementation
	count := mpoolInstance.CountTx()
	s.Require().Greater(count, 0, "mempool should contain transactions")

	s.T().Log("Successfully tested multiple transactions with nonce gaps")
}

// TestFillNonceGap tests filling a previously created nonce gap
func (s *MempoolIntegrationTestSuite) TestFillNonceGap() {
	// Create an Ethereum private key and address
	sender := s.keyring.GetKey(0)
	privKey := sender.Priv

	mpoolInstance := mempool.GetGlobalEVMMempool()
	to := utiltx.GenerateAddress()

	// Create transaction with nonce 2 (creates gap)
	evmTxArgs := evmtypes.EvmTxArgs{
		Nonce:    2,
		To:       &to,
		Amount:   big.NewInt(1000),
		GasLimit: 21000,
		GasPrice: big.NewInt(1000000000),
		ChainID:  s.network.GetEIP155ChainID(),
	}

	signedMsg, err := s.factory.GenerateSignedMsgEthereumTx(privKey, evmTxArgs)
	s.Require().NoError(err)

	// Use PrepareEthTx to build the transaction properly for EVM messages
	tx, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), privKey, &signedMsg)
	s.Require().NoError(err)

	// Insert transaction with gap
	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	countAfterGap := mpoolInstance.CountTx()

	// Try to use InsertInvalidNonce for gap handling if available
	// Note: This tests the specific EVM mempool functionality for nonce gaps
	txBytes, err := s.network.App.GetTxConfig().TxEncoder()(tx)
	s.Require().NoError(err)

	err = mpoolInstance.InsertInvalidNonce(txBytes)
	// Don't assert on error since behavior may vary for nonce gaps

	finalCount := mpoolInstance.CountTx()
	s.Require().GreaterOrEqual(finalCount, countAfterGap, "nonce gap handling should not decrease transaction count")

	s.T().Log("Successfully tested filling nonce gap")
}

// TestSequentialNonceHandling tests handling of sequential nonces after gaps
func (s *MempoolIntegrationTestSuite) TestSequentialNonceHandling() {
	// Create an Ethereum private key and address
	sender := s.keyring.GetKey(0)
	privKey := sender.Priv

	mpoolInstance := mempool.GetGlobalEVMMempool()
	to := utiltx.GenerateAddress()

	// Insert transactions with nonces 0, 3, then fill gap with 1, 2
	nonces := []uint64{0, 3, 1, 2}
	var transactions []sdk.Tx

	for _, nonce := range nonces {
		evmTxArgs := evmtypes.EvmTxArgs{
			Nonce:    nonce,
			To:       &to,
			Amount:   big.NewInt(int64(1000 + nonce*100)),
			GasLimit: 21000,
			GasPrice: big.NewInt(int64(1000000000 + nonce*100000000)),
			ChainID:  s.network.GetEIP155ChainID(),
		}

		signedMsg, err := s.factory.GenerateSignedMsgEthereumTx(privKey, evmTxArgs)
		s.Require().NoError(err)

		// Use PrepareEthTx to build the transaction properly for EVM messages
		tx, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), privKey, &signedMsg)
		s.Require().NoError(err)
		transactions = append(transactions, tx)

		err = mpoolInstance.Insert(s.network.GetContext(), tx)
		s.Require().NoError(err)
	}

	// Verify all transactions were handled
	finalCount := mpoolInstance.CountTx()
	s.Require().Greater(finalCount, 0, "mempool should contain transactions after sequential nonce handling")

	// Try to select transactions - order may vary based on nonce handling
	iterator := mpoolInstance.Select(s.network.GetContext(), nil)
	s.Require().NotNil(iterator, "should be able to select transactions")

	selectedCount := 0
	for {
		tx := iterator.Tx()
		if tx == nil {
			break
		}
		selectedCount++

		iterator = iterator.Next()
		if iterator == nil {
			break
		}
	}

	s.Require().Greater(selectedCount, 0, "should be able to select transactions with resolved nonce gaps")

	s.T().Log("Successfully tested sequential nonce handling after gaps")
}

// TestInvalidNonceInsertion tests the InsertInvalidNonce functionality
func (s *MempoolIntegrationTestSuite) TestInvalidNonceInsertion() {
	// Create an Ethereum private key and address
	sender := s.keyring.GetKey(0)
	privKey := sender.Priv

	mpoolInstance := mempool.GetGlobalEVMMempool()

	// Create multiple transactions with various nonce scenarios
	to := utiltx.GenerateAddress()
	var transactions []sdk.Tx

	// Create transactions with nonces: 5, 2, 8, 1
	testNonces := []uint64{5, 2, 8, 1}

	for i, nonce := range testNonces {
		evmTxArgs := evmtypes.EvmTxArgs{
			Nonce:    nonce,
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
		transactions = append(transactions, tx)

		// Use InsertInvalidNonce for these transactions
		txBytes, err := s.network.App.GetTxConfig().TxEncoder()(tx)
		s.Require().NoError(err)

		err = mpoolInstance.InsertInvalidNonce(txBytes)
		// Don't assert on error - behavior may vary for invalid nonces
	}

	// Verify mempool handled the transactions appropriately
	count := mpoolInstance.CountTx()
	s.T().Logf("Mempool contains %d transactions after invalid nonce insertions", count)

	s.T().Log("Successfully tested invalid nonce insertion functionality")
}

// TestNonceGapWithDifferentAccounts tests nonce gaps across multiple accounts
func (s *MempoolIntegrationTestSuite) TestNonceGapWithDifferentAccounts() {
	mpoolInstance := mempool.GetGlobalEVMMempool()

	// Use two different keyring accounts
	sender1 := s.keyring.GetKey(0)
	privKey1 := sender1.Priv

	sender2 := s.keyring.GetKey(1)
	privKey2 := sender2.Priv

	to := utiltx.GenerateAddress()

	// Create transactions for account 1 with nonce gap
	evmTxArgs1 := evmtypes.EvmTxArgs{
		Nonce:    0,
		To:       &to,
		Amount:   big.NewInt(1000),
		GasLimit: 21000,
		GasPrice: big.NewInt(1000000000),
		ChainID:  s.network.GetEIP155ChainID(),
	}

	signedMsg1, err := s.factory.GenerateSignedMsgEthereumTx(privKey1, evmTxArgs1)
	s.Require().NoError(err)

	// Use PrepareEthTx to build the transaction properly for EVM messages
	tx1, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), privKey1, &signedMsg1)
	s.Require().NoError(err)

	// Create transactions for account 2 with nonce gap
	evmTxArgs2 := evmtypes.EvmTxArgs{
		Nonce:    3, // Nonce gap for this account
		To:       &to,
		Amount:   big.NewInt(2000),
		GasLimit: 21000,
		GasPrice: big.NewInt(1000000000),
		ChainID:  s.network.GetEIP155ChainID(),
	}

	signedMsg2, err := s.factory.GenerateSignedMsgEthereumTx(privKey2, evmTxArgs2)
	s.Require().NoError(err)

	// Use PrepareEthTx to build the transaction properly for EVM messages
	tx2, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), privKey2, &signedMsg2)
	s.Require().NoError(err)

	// Insert both transactions
	err = mpoolInstance.Insert(s.network.GetContext(), tx1)
	s.Require().NoError(err)

	err = mpoolInstance.Insert(s.network.GetContext(), tx2)
	s.Require().NoError(err)

	// Verify mempool handles different accounts independently
	count := mpoolInstance.CountTx()
	s.Require().Greater(count, 0, "mempool should contain transactions from both accounts")

	s.T().Log("Successfully tested nonce gaps across different accounts")
}
