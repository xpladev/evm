package mempool

import (
	"fmt"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"
	"github.com/cosmos/evm/mempool/miner"
	"github.com/cosmos/evm/mempool/txpool"
	types2 "github.com/cosmos/evm/x/vm/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"math/big"
)

var _ mempool.Iterator = &EVMMempoolIterator{}

type EVMMempoolIterator struct {
	/** Mempool Iterators **/
	evmIterator    *miner.TransactionsByPriceAndNonce
	cosmosIterator mempool.Iterator

	/** Utils **/
	txConfig client.TxConfig

	/** Chain Params **/
	bondDenom string
	chainID   *big.Int

	/** State tracking **/
	lastTxWasEVM bool
	lastTxHash   string
}

func NewEVMMempoolIterator(evmIterator *miner.TransactionsByPriceAndNonce, cosmosIterator mempool.Iterator, txConfig client.TxConfig, bondDenom string, chainId *big.Int) mempool.Iterator {
	// Check if we have any transactions at all
	hasEVM := evmIterator != nil && !evmIterator.Empty()
	hasCosmos := cosmosIterator != nil && cosmosIterator.Tx() != nil

	if !hasEVM && !hasCosmos {
		fmt.Println("New Iterator is Empty")
		return nil
	}

	return &EVMMempoolIterator{
		evmIterator:    evmIterator,
		cosmosIterator: cosmosIterator,
		txConfig:       txConfig,
		bondDenom:      bondDenom,
		chainID:        chainId,
	}
}

// shouldUseEVM returns true if EVM transaction should be used, false for Cosmos
func (i *EVMMempoolIterator) shouldUseEVM() bool {
	var nextEVMTx *txpool.LazyTransaction
	var evmFee *uint256.Int
	if i.evmIterator != nil {
		nextEVMTx, evmFee = i.evmIterator.Peek()
	}

	var nextCosmosTx sdk.Tx
	if i.cosmosIterator != nil {
		nextCosmosTx = i.cosmosIterator.Tx()
	}

	// If only one type available
	if nextEVMTx == nil {
		return false // Use Cosmos
	}
	if nextCosmosTx == nil {
		return true // Use EVM
	}

	// Both have transactions - compare fees
	cosmosTxFee, ok := nextCosmosTx.(sdk.FeeTx)
	if !ok {
		return true // Use EVM if Cosmos tx has no fee
	}

	cosmosFees := cosmosTxFee.GetFee()
	var cosmosTxEVMDenomFee *sdk.Coin
	for _, coin := range cosmosFees {
		if coin.Denom == i.bondDenom {
			cosmosTxEVMDenomFee = &coin
			break
		}
	}

	if cosmosTxEVMDenomFee == nil {
		return true // Use EVM if Cosmos tx has wrong denomination
	}

	cosmosTxAmount, overflow := uint256.FromBig(cosmosTxEVMDenomFee.Amount.BigInt())
	if overflow || !cosmosTxAmount.Gt(evmFee) {
		return true // Use EVM if Cosmos fee is not higher
	}

	return false // Use Cosmos if it has higher fee
}

func (i *EVMMempoolIterator) Next() mempool.Iterator {
	// Determine what transaction we just returned and advance the correct iterator
	var nextEVMTx *txpool.LazyTransaction
	if i.evmIterator != nil {
		nextEVMTx, _ = i.evmIterator.Peek()
	}

	var nextCosmosTx sdk.Tx
	if i.cosmosIterator != nil {
		nextCosmosTx = i.cosmosIterator.Tx()
	}

	// If no transactions available, we're done
	if nextEVMTx == nil && nextCosmosTx == nil {
		return nil
	}

	// Advance the iterator that provided the current transaction
	// NOTE: We don't modify the EVM pool during iteration - transactions
	// will be removed later via Remove() after block execution
	if i.shouldUseEVM() {
		if nextEVMTx != nil {
			// We used EVM transaction, advance EVM iterator
			if i.evmIterator != nil {
				fmt.Printf("Shifting EVM tx during selection: %x\n", nextEVMTx.Tx.Hash())
				i.evmIterator.Shift()
			}
		} else {
			// No EVM tx, we used cosmos, advance cosmos
			if i.cosmosIterator != nil {
				i.cosmosIterator = i.cosmosIterator.Next()
			}
		}
	} else {
		// We used cosmos transaction, advance cosmos iterator
		if i.cosmosIterator != nil {
			i.cosmosIterator = i.cosmosIterator.Next()
		}
	}

	// Check if we still have transactions after advancing
	var hasMoreEVM, hasMoreCosmos bool
	if i.evmIterator != nil {
		nextEVMTx, _ = i.evmIterator.Peek()
		hasMoreEVM = nextEVMTx != nil
	}
	if i.cosmosIterator != nil {
		hasMoreCosmos = i.cosmosIterator.Tx() != nil
	}

	if !hasMoreEVM && !hasMoreCosmos {
		return nil
	}

	return i
}

// convertEVMToSDKTx converts an EVM transaction to SDK transaction
func (i *EVMMempoolIterator) convertEVMToSDKTx(nextEVMTx *txpool.LazyTransaction) sdk.Tx {
	if nextEVMTx == nil {
		return nil
	}
	msgEthereumTx := &types2.MsgEthereumTx{}
	if err := msgEthereumTx.FromSignedEthereumTx(nextEVMTx.Tx, ethtypes.LatestSignerForChainID(i.chainID)); err != nil {
		return nil // Return nil for invalid tx instead of panicking
	}
	cosmosTx, err := msgEthereumTx.BuildTx(i.txConfig.NewTxBuilder(), i.bondDenom)
	if err != nil {
		return nil
	}
	return cosmosTx
}

func (i *EVMMempoolIterator) Tx() sdk.Tx {
	// Get current transactions from both iterators
	var nextEVMTx *txpool.LazyTransaction
	if i.evmIterator != nil {
		nextEVMTx, _ = i.evmIterator.Peek()
	}

	var nextCosmosTx sdk.Tx
	if i.cosmosIterator != nil {
		nextCosmosTx = i.cosmosIterator.Tx()
	}

	// If no transactions available, this shouldn't happen if iterator was created properly
	if nextEVMTx == nil && nextCosmosTx == nil {
		return nil
	}

	// Return the appropriate transaction based on priority
	if i.shouldUseEVM() {
		if nextEVMTx != nil {
			evmTx := i.convertEVMToSDKTx(nextEVMTx)
			fmt.Println("TX", nextEVMTx.Tx.Hash())
			if evmTx != nil {
				// Track that we're returning an EVM transaction
				i.lastTxWasEVM = true
				i.lastTxHash = nextEVMTx.Tx.Hash().Hex()
				return evmTx
			}
		}
		// Fall back to cosmos if EVM fails
		i.lastTxWasEVM = false
		i.lastTxHash = ""
		return nextCosmosTx
	} else {
		i.lastTxWasEVM = false
		i.lastTxHash = ""
		return nextCosmosTx
	}
}
