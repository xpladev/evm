package mempool

import (
	"math/big"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	mempool "github.com/cosmos/evm/mempool"
	basefactory "github.com/cosmos/evm/testutil/integration/base/factory"
	utiltx "github.com/cosmos/evm/testutil/tx"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// TestSelectEmptyMempool tests selection from an empty mempool
func (s *MempoolIntegrationTestSuite) TestSelectEmptyMempool() {
	mpoolInstance := mempool.GetGlobalEVMMempool()

	// Select from empty mempool
	iterator := mpoolInstance.Select(s.network.GetContext(), nil)
	s.Require().Nil(iterator, "iterator should be nil for empty mempool")

	s.T().Log("Successfully selected from empty mempool")
}

// TestSelectSingleTransaction tests selection with a single transaction
func (s *MempoolIntegrationTestSuite) TestSelectSingleTransaction() {
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Fund the sender
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

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

	mpoolInstance := mempool.GetGlobalEVMMempool()
	err = mpoolInstance.Insert(s.network.GetContext(), tx)
	s.Require().NoError(err)

	// Select transactions
	iterator := mpoolInstance.Select(s.network.GetContext(), nil)
	s.Require().NotNil(iterator, "iterator should not be nil")

	// Verify we can get the transaction
	selectedTx := iterator.Tx()
	s.Require().NotNil(selectedTx, "should be able to select the inserted transaction")

	// Verify transaction matches what we inserted
	s.Require().Equal(tx, selectedTx, "selected transaction should match inserted transaction")

	s.T().Log("Successfully selected single transaction from mempool")
}

// TestSelectMultipleTransactions tests selection with multiple transactions
func (s *MempoolIntegrationTestSuite) TestSelectMultipleTransactions() {
	sender1 := s.keyring.GetKey(0)
	sender2 := s.keyring.GetKey(1)
	recipient := s.keyring.GetKey(2)

	// Fund both senders
	s.FundAccount(sender1.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())
	s.FundAccount(sender2.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	mpoolInstance := mempool.GetGlobalEVMMempool()

	// Create and insert multiple transactions
	var insertedTxs []sdk.Tx

	// Transaction 1 with higher fee (should be prioritized)
	bankMsg1 := banktypes.NewMsgSend(
		sender1.AccAddr,
		recipient.AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000))),
	)

	tx1, err := s.factory.BuildCosmosTx(sender1.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{bankMsg1},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(2000000000000000))), // Higher fee
	})
	s.Require().NoError(err)
	insertedTxs = append(insertedTxs, tx1)

	// Transaction 2 with lower fee
	bankMsg2 := banktypes.NewMsgSend(
		sender2.AccAddr,
		recipient.AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(2000))),
	)

	tx2, err := s.factory.BuildCosmosTx(sender2.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{bankMsg2},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000000000000000))), // Lower fee
	})
	s.Require().NoError(err)
	insertedTxs = append(insertedTxs, tx2)

	// Insert transactions
	for _, tx := range insertedTxs {
		err = mpoolInstance.Insert(s.network.GetContext(), tx)
		s.Require().NoError(err)
	}

	// Select transactions
	iterator := mpoolInstance.Select(s.network.GetContext(), nil)
	s.Require().NotNil(iterator, "iterator should not be nil")

	// Collect all selected transactions
	var selectedTxs []sdk.Tx
	for {
		tx := iterator.Tx()
		if tx == nil {
			break
		}
		selectedTxs = append(selectedTxs, tx)

		iterator = iterator.Next()
		if iterator == nil {
			break
		}
	}

	// Verify we got all transactions
	s.Require().Len(selectedTxs, len(insertedTxs), "should select all inserted transactions")

	// Note: The exact order depends on the mempool's prioritization algorithm
	// We just verify that all transactions are present
	for _, insertedTx := range insertedTxs {
		found := false
		for _, selectedTx := range selectedTxs {
			if insertedTx == selectedTx {
				found = true
				break
			}
		}
		s.Require().True(found, "all inserted transactions should be selected")
	}

	s.T().Log("Successfully selected multiple transactions from mempool")
}

// TestSelectWithMaxBytes tests selection with byte limit
func (s *MempoolIntegrationTestSuite) TestSelectWithMaxBytes() {
	sender := s.keyring.GetKey(0)
	recipient := s.keyring.GetKey(1)

	// Fund the sender
	s.FundAccount(sender.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	// Create multiple transactions
	mpoolInstance := mempool.GetGlobalEVMMempool()
	var insertedTxs []sdk.Tx

	for i := 0; i < 3; i++ {
		bankMsg := banktypes.NewMsgSend(
			sender.AccAddr,
			recipient.AccAddr,
			sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(int64(1000+i)))),
		)

		tx, err := s.factory.BuildCosmosTx(sender.Priv, basefactory.CosmosTxArgs{
			Msgs: []sdk.Msg{bankMsg},
			Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(int64(1000000000000000+i*100000000000)))),
			// Different sequence numbers for each transaction
		})
		s.Require().NoError(err)
		insertedTxs = append(insertedTxs, tx)

		err = mpoolInstance.Insert(s.network.GetContext(), tx)
		s.Require().NoError(err)
	}

	// Create a very small byte limit to test selection limits
	txBytes, err := s.network.App.GetTxConfig().TxEncoder()(insertedTxs[0])
	s.Require().NoError(err)
	smallLimit := len(txBytes) / 2 // Less than one transaction size

	// Select with byte limit
	iterator := mpoolInstance.Select(s.network.GetContext(), [][]byte{txBytes[:smallLimit]})
	s.Require().NotNil(iterator, "iterator should not be nil")

	// The behavior with byte limits depends on implementation
	// We just verify it doesn't crash and returns a valid iterator

	s.T().Log("Successfully tested selection with byte limit")
}

// TestSelectEVMTransactions tests selection of EVM transactions
func (s *MempoolIntegrationTestSuite) TestSelectEVMTransactions() {
	// Use a keyring account that's already funded in genesis
	sender := s.keyring.GetKey(0)
	privKey := sender.Priv

	// Create EVM transactions with different nonces
	mpoolInstance := mempool.GetGlobalEVMMempool()
	to := utiltx.GenerateAddress()
	var insertedTxs []sdk.Tx

	for i := 0; i < 3; i++ {
		evmTxArgs := evmtypes.EvmTxArgs{
			Nonce:    uint64(i),
			To:       &to,
			Amount:   big.NewInt(int64(1000 + i)),
			GasLimit: 21000,
			GasPrice: big.NewInt(int64(1000000000 + i*100000000)), // Different gas prices
			ChainID:  s.network.GetEIP155ChainID(),
		}

		// Use factory to create and sign the transaction
		signedMsg, err := s.factory.GenerateSignedMsgEthereumTx(privKey, evmTxArgs)
		s.Require().NoError(err)

		// Use PrepareEthTx to build the transaction properly for EVM messages
		tx, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), privKey, &signedMsg)
		s.Require().NoError(err)
		insertedTxs = append(insertedTxs, tx)

		err = mpoolInstance.Insert(s.network.GetContext(), tx)
		s.Require().NoError(err)
	}

	// Select transactions
	iterator := mpoolInstance.Select(s.network.GetContext(), nil)
	s.Require().NotNil(iterator, "iterator should not be nil")

	// Verify we can iterate through EVM transactions
	txCount := 0
	for {
		tx := iterator.Tx()
		if tx == nil {
			break
		}

		// Verify it's an EVM transaction
		msgs := tx.GetMsgs()
		s.Require().Len(msgs, 1, "EVM transaction should have exactly one message")

		_, ok := msgs[0].(*evmtypes.MsgEthereumTx)
		s.Require().True(ok, "message should be an EVM transaction")

		txCount++

		iterator = iterator.Next()
		if iterator == nil {
			break
		}
	}

	s.Require().Greater(txCount, 0, "should select at least one EVM transaction")

	s.T().Log("Successfully selected EVM transactions from mempool")
}

// TestSelectByFunction tests the SelectBy method with custom filtering
func (s *MempoolIntegrationTestSuite) TestSelectByFunction() {
	sender1 := s.keyring.GetKey(0)
	sender2 := s.keyring.GetKey(1)
	recipient := s.keyring.GetKey(2)

	// Fund both senders
	s.FundAccount(sender1.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())
	s.FundAccount(sender2.AccAddr, sdkmath.NewInt(2000000000000000000), s.network.GetBaseDenom())

	mpoolInstance := mempool.GetGlobalEVMMempool()

	// Create transactions with different amounts
	bankMsg1 := banktypes.NewMsgSend(
		sender1.AccAddr,
		recipient.AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000))), // Small amount
	)

	tx1, err := s.factory.BuildCosmosTx(sender1.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{bankMsg1},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000000000000000))),
	})
	s.Require().NoError(err)

	bankMsg2 := banktypes.NewMsgSend(
		sender2.AccAddr,
		recipient.AccAddr,
		sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(5000))), // Large amount
	)

	tx2, err := s.factory.BuildCosmosTx(sender2.Priv, basefactory.CosmosTxArgs{
		Msgs: []sdk.Msg{bankMsg2},
		Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(1000000000000000))),
		// Different sequence for second transaction
	})
	s.Require().NoError(err)

	// Insert both transactions
	err = mpoolInstance.Insert(s.network.GetContext(), tx1)
	s.Require().NoError(err)
	err = mpoolInstance.Insert(s.network.GetContext(), tx2)
	s.Require().NoError(err)

	// Test SelectBy with filtering function
	filteredCount := 0
	filterFunc := func(tx sdk.Tx) bool {
		// Filter for transactions with bank send messages
		msgs := tx.GetMsgs()
		if len(msgs) != 1 {
			return false
		}

		bankMsg, ok := msgs[0].(*banktypes.MsgSend)
		if !ok {
			return false
		}

		// Only select transactions with amount >= 5000
		return bankMsg.Amount.AmountOf(s.network.GetBaseDenom()).GTE(sdkmath.NewInt(5000))
	}

	// Note: SelectBy doesn't return anything, but we can use it to test filtering logic
	mpoolInstance.SelectBy(s.network.GetContext(), nil, func(tx sdk.Tx) bool {
		if filterFunc(tx) {
			filteredCount++
		}
		return true // Continue iteration
	})

	// We should have found at least one transaction that matches our filter
	s.Require().Greater(filteredCount, 0, "should find at least one transaction matching filter")

	s.T().Log("Successfully tested SelectBy functionality")
}
