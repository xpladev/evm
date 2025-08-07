package mempool

import (
	"math/big"
	"sync"

	utiltx "github.com/cosmos/evm/testutil/tx"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// TestConcurrentEVMTransactions tests concurrent EVM transaction operations using txBuilder
func (s *MempoolIntegrationTestSuite) TestConcurrentEVMTransactions() {
	mpoolInstance := s.network.App.GetMempool()

	numWorkers := 3
	numTxsPerWorker := 10

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
	s.Require().Equal(finalCount, 30, "mempool should contain EVM transactions after concurrent insertion")

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

	s.Require().Equal(evmTxCount, 30, "should have selected EVM transactions")
	s.T().Logf("Successfully processed %d EVM transactions concurrently", evmTxCount)
}
