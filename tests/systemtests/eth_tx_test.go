package systemtests

/*
import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
	"time"

	"cosmossdk.io/systemtests"
	"github.com/cosmos/evm/testutil/integration/evm"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

func TestNonceGappedTxs(t *testing.T) {
	sut := systemtests.Sut
	sut.ResetChain(t)

	// Generate a test account
	privKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	toAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Fund the test account
	fundingAmount := big.NewInt(1000000000000000000) // 1 ETH in wei
	fundAccount(t, sut, fromAddr, fundingAmount)

	// Get current nonce for the account
	currentNonce := getCurrentNonce(t, sut, fromAddr)

	// Create transaction with nonce+1 (creating a gap)
	gappedTx := createTransaction(t, privKey, toAddr, big.NewInt(100000000000000000), currentNonce+1) // 0.1 ETH

	// Submit the gapped transaction
	gappedTxHash := submitTransaction(t, sut, gappedTx)
	t.Logf("Submitted gapped transaction (nonce %d): %s", currentNonce+1, gappedTxHash)

	// Wait a moment to ensure the transaction is in mempool
	time.Sleep(2 * time.Second)

	// Create transaction with nonce+0 (filling the gap)
	missingTx := createTransaction(t, privKey, toAddr, big.NewInt(50000000000000000), currentNonce) // 0.05 ETH

	// Submit the missing transaction
	missingTxHash := submitTransaction(t, sut, missingTx)
	t.Logf("Submitted missing transaction (nonce %d): %s", currentNonce, missingTxHash)

	// Wait for both transactions to be included in blocks
	waitForTransaction(t, sut, gappedTxHash)
	waitForTransaction(t, sut, missingTxHash)

	// Verify both transactions succeeded
	verifyTransactionSuccess(t, sut, gappedTxHash)
	verifyTransactionSuccess(t, sut, missingTxHash)

	t.Log("Both nonce-gapped transactions succeeded")
}

// fundAccount funds the given address with the specified amount
func fundAccount(t *testing.T, sut *systemtests.SystemUnderTest, addr common.Address, amount *big.Int) {
	// Use the predefined validator account to fund our test account
	validatorKey := getValidatorPrivateKey(t)
	validatorAddr := crypto.PubkeyToAddress(validatorKey.PublicKey)

	// Get validator's current nonce
	validatorNonce := getCurrentNonce(t, sut, validatorAddr)

	// Create funding transaction
	fundingTx := createTransaction(t, validatorKey, addr, amount, validatorNonce)

	// Submit and wait for funding transaction
	fundingTxHash := submitTransaction(t, sut, fundingTx)
	waitForTransaction(t, sut, fundingTxHash)
	verifyTransactionSuccess(t, sut, fundingTxHash)

	t.Logf("Funded account %s with %s wei", addr.Hex(), amount.String())
}

// getValidatorPrivateKey returns a private key for one of the validators
func getValidatorPrivateKey(t *testing.T) *ecdsa.PrivateKey {
	// Use the first mnemonic from the testnet configuration
	mnemonic := "copper push brief egg scan entry inform record adjust fossil boss egg comic alien upon aspect dry avoid interest fury window hint race symptom"

	privKey, err := evm.DerivePrivKeyFromMnemonic(mnemonic, 0)
	if err != nil {
		t.Fatalf("Failed to derive private key from mnemonic: %v", err)
	}

	return privKey
}

// getCurrentNonce gets the current nonce for an address
func getCurrentNonce(t *testing.T, sut *systemtests.SystemTestSuite, addr common.Address) uint64 {
	ctx := context.Background()

	// Call eth_getTransactionCount via JSON-RPC
	result := sut.RPCClient().CallJSON(ctx, "eth_getTransactionCount", addr.Hex(), "latest")

	var nonceHex string
	err := json.Unmarshal(result, &nonceHex)
	if err != nil {
		t.Fatalf("Failed to unmarshal nonce response: %v", err)
	}

	nonce, err := hexutil.DecodeUint64(nonceHex)
	if err != nil {
		t.Fatalf("Failed to decode nonce: %v", err)
	}

	return nonce
}

// createTransaction creates a signed Ethereum transaction
func createTransaction(t *testing.T, privKey *ecdsa.PrivateKey, to common.Address, value *big.Int, nonce uint64) *ethtypes.Transaction {
	gasLimit := uint64(21000)
	gasPrice := big.NewInt(20000000000) // 20 gwei

	tx := ethtypes.NewTransaction(nonce, to, value, gasLimit, gasPrice, nil)

	// Sign the transaction
	chainID := big.NewInt(9000) // EVM chain ID from testnet config
	signer := ethtypes.NewEIP155Signer(chainID)

	signedTx, err := ethtypes.SignTx(tx, signer, privKey)
	if err != nil {
		t.Fatalf("Failed to sign transaction: %v", err)
	}

	return signedTx
}

// submitTransaction submits a transaction via eth_sendRawTransaction
func submitTransaction(t *testing.T, sut *systemtests.SystemTestSuite, tx *ethtypes.Transaction) string {
	ctx := context.Background()

	// Encode the transaction as raw bytes
	txBytes, err := tx.MarshalBinary()
	if err != nil {
		t.Fatalf("Failed to marshal transaction: %v", err)
	}

	rawTxHex := hexutil.Encode(txBytes)

	// Submit via eth_sendRawTransaction
	result := sut.RPCClient().CallJSON(ctx, "eth_sendRawTransaction", rawTxHex)

	var txHash string
	err = json.Unmarshal(result, &txHash)
	if err != nil {
		t.Fatalf("Failed to unmarshal transaction hash: %v", err)
	}

	return txHash
}

// waitForTransaction waits for a transaction to be included in a block
func waitForTransaction(t *testing.T, sut *systemtests.SystemTestSuite, txHash string) {
	ctx := context.Background()
	timeout := time.Now().Add(30 * time.Second)

	for time.Now().Before(timeout) {
		result := sut.RPCClient().CallJSON(ctx, "eth_getTransactionReceipt", txHash)

		var receipt map[string]interface{}
		err := json.Unmarshal(result, &receipt)
		if err == nil && receipt != nil {
			return // Transaction found
		}

		time.Sleep(1 * time.Second)
	}

	t.Fatalf("Transaction %s not found within timeout", txHash)
}

// verifyTransactionSuccess verifies that a transaction was successful
func verifyTransactionSuccess(t *testing.T, sut *systemtests.SystemTestSuite, txHash string) {
	ctx := context.Background()

	result := sut.RPCClient().CallJSON(ctx, "eth_getTransactionReceipt", txHash)

	var receipt map[string]interface{}
	err := json.Unmarshal(result, &receipt)
	if err != nil {
		t.Fatalf("Failed to get transaction receipt for %s: %v", txHash, err)
	}

	if receipt == nil {
		t.Fatalf("Transaction receipt not found for %s", txHash)
	}

	status, ok := receipt["status"].(string)
	if !ok || status != "0x1" {
		t.Fatalf("Transaction %s failed with status: %v", txHash, status)
	}

	t.Logf("Transaction %s succeeded", txHash)
}

*/
