package mempool

import (
	"math/big"
	"sync"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	mempool "github.com/cosmos/evm/mempool"
	basefactory "github.com/cosmos/evm/testutil/integration/base/factory"
	utiltx "github.com/cosmos/evm/testutil/tx"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// TestConcurrentInsertion tests concurrent insertion of transactions
func (s *MempoolIntegrationTestSuite) TestConcurrentInsertion() {
	mpoolInstance := mempool.GetGlobalEVMMempool()

	// Create multiple accounts for concurrent operations
	numWorkers := 3 // Limited by keyring size
	numTxsPerWorker := 10

	var wg sync.WaitGroup
	var mu sync.Mutex
	var insertErrors []error

	for workerID := 0; workerID < numWorkers; workerID++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			// Create unique sender for this worker
			sender := s.keyring.GetKey(id)
			recipient := s.keyring.GetKey((id + 1) % numWorkers)

			// Fund the sender (synchronized to prevent race conditions)
			fundAmount, _ := sdkmath.NewIntFromString("5000000000000000000")
			// Use a mutex to prevent concurrent funding issues
			mu.Lock()
			s.FundAccount(sender.AccAddr, fundAmount, s.network.GetBaseDenom())
			mu.Unlock()

			// Insert multiple transactions concurrently
			for txIdx := 0; txIdx < numTxsPerWorker; txIdx++ {
				bankMsg := banktypes.NewMsgSend(
					sender.AccAddr,
					recipient.AccAddr,
					sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(int64(100+txIdx)))),
				)

				tx, err := s.factory.BuildCosmosTx(sender.Priv, basefactory.CosmosTxArgs{
					Msgs: []sdk.Msg{bankMsg},
					Fees: sdk.NewCoins(sdk.NewCoin(s.network.GetBaseDenom(), sdkmath.NewInt(int64(1000000000000000+txIdx*1000)))),
				})

				if err != nil {
					mu.Lock()
					insertErrors = append(insertErrors, err)
					mu.Unlock()
					continue
				}

				err = mpoolInstance.Insert(s.network.GetContext(), tx)
				if err != nil {
					mu.Lock()
					insertErrors = append(insertErrors, err)
					mu.Unlock()
				}
			}
		}(workerID)
	}

	// Wait for all workers to complete
	wg.Wait()

	// Verify no critical errors occurred
	s.Require().Empty(insertErrors, "should not have insertion errors during concurrent operations")

	// Verify transactions were inserted
	finalCount := mpoolInstance.CountTx()
	s.Require().Greater(finalCount, 0, "mempool should contain transactions after concurrent insertion")

	s.T().Logf("Successfully inserted transactions concurrently, final count: %d", finalCount)
}

// TestConcurrentEVMTransactions tests concurrent EVM transaction operations using txBuilder
func (s *MempoolIntegrationTestSuite) TestConcurrentEVMTransactions() {
	mpoolInstance := mempool.GetGlobalEVMMempool()

	numWorkers := 3
	numTxsPerWorker := 5

	var wg sync.WaitGroup
	var mu sync.Mutex
	var evmErrors []error

	for workerID := 0; workerID < numWorkers; workerID++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			// Use a keyring account for this worker (already funded in genesis)
			sender := s.keyring.GetKey(id)
			privKey := sender.Priv

			to := utiltx.GenerateAddress()

			// Create multiple EVM transactions
			for nonce := 0; nonce < numTxsPerWorker; nonce++ {
				evmTxArgs := evmtypes.EvmTxArgs{
					Nonce:    uint64(nonce),
					To:       &to,
					Amount:   big.NewInt(int64(1000 + id*100 + nonce)),
					GasLimit: 21000,
					GasPrice: big.NewInt(int64(1000000000 + id*100000000 + nonce*10000000)),
					ChainID:  s.network.GetEIP155ChainID(),
				}

				// Create the MsgEthereumTx message
				signedMsg, err := s.factory.GenerateSignedMsgEthereumTx(privKey, evmTxArgs)
				if err != nil {
					mu.Lock()
					evmErrors = append(evmErrors, err)
					mu.Unlock()
					continue
				}

				// Use PrepareEthTx to build the transaction properly for EVM messages
				tx, err := utiltx.PrepareEthTx(s.network.App.GetTxConfig(), privKey, &signedMsg)
				if err != nil {
					mu.Lock()
					evmErrors = append(evmErrors, err)
					mu.Unlock()
					continue
				}

				err = mpoolInstance.Insert(s.network.GetContext(), tx)
				if err != nil {
					mu.Lock()
					evmErrors = append(evmErrors, err)
					mu.Unlock()
				}
			}
		}(workerID)
	}

	wg.Wait()

	// Verify no critical errors occurred
	s.Require().Empty(evmErrors, "should not have errors during concurrent EVM operations")

	// Verify EVM transactions were inserted
	finalCount := mpoolInstance.CountTx()
	s.Require().Greater(finalCount, 0, "mempool should contain EVM transactions after concurrent insertion")

	// Verify we can select EVM transactions
	iterator := mpoolInstance.Select(s.network.GetContext(), nil)
	s.Require().NotNil(iterator, "should be able to select after concurrent EVM insertion")

	evmTxCount := 0
	for {
		tx := iterator.Tx()
		if tx == nil {
			break
		}

		// Check if it's an EVM transaction
		msgs := tx.GetMsgs()
		if len(msgs) > 0 {
			if _, ok := msgs[0].(*evmtypes.MsgEthereumTx); ok {
				evmTxCount++
			}
		}

		iterator = iterator.Next()
		if iterator == nil {
			break
		}
	}

	s.Require().Greater(evmTxCount, 0, "should have selected EVM transactions")
	s.T().Logf("Successfully processed %d EVM transactions concurrently", evmTxCount)
}
