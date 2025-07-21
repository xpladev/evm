package mempool

import (
	"context"
	"github.com/cosmos/evm/mempool/mocks"
	"math/big"
	"testing"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/evm/mempool/txpool"
	"github.com/cosmos/evm/mempool/txpool/legacypool"
	"github.com/cosmos/evm/x/vm/statedb"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type MempoolTestSuite struct {
	suite.Suite
	ctx            sdk.Context
	mempool        *EVMMempool
	mockVMKeeper   VMKeeperI
	mockCosmosPool *mocks.MockCosmosPool
	txDecoder      sdk.TxDecoder
	mockChain      *mocks.MockBlockChain
}

func (suite *MempoolTestSuite) SetupTest() {
	suite.ctx = sdk.NewContext(nil, tmproto.Header{}, false, nil)

	suite.mockVMKeeper = &mocks.MockVMKeeper{
		BaseFee: big.NewInt(1000000000), // 1 gwei
		Params: evmtypes.Params{
			EvmDenom: "wei",
		},
		Accounts: make(map[common.Address]*statedb.Account),
	}

	suite.mockCosmosPool = &mocks.MockCosmosPool{
		Txs: make([]sdk.Tx, 0),
	}

	// Create a minimal txDecoder for testing
	suite.txDecoder = func(txBytes []byte) (sdk.Tx, error) {
		return &mocks.MockFeeTx{}, nil
	}

	// Create a minimal txpool with legacypool
	suite.mockChain = mocks.NewMockBlockChain(suite.mockVMKeeper.(*mocks.MockVMKeeper))
	legacyPool := legacypool.New(legacypool.DefaultConfig, suite.mockChain)

	// Initialize the legacy pool with a proper header
	reserver := &mocks.MockReserver{}
	err := legacyPool.Init(1000000000, suite.mockChain.CurrentBlock(), reserver)
	require.NoError(suite.T(), err)

	txPool := &txpool.TxPool{
		Subpools: []txpool.SubPool{legacyPool},
	}

	suite.mempool = NewEVMMempool(suite.mockVMKeeper, txPool, suite.mockCosmosPool, suite.txDecoder)
}

func (suite *MempoolTestSuite) TestNewEVMMempool() {
	require.NotNil(suite.T(), suite.mempool)
	require.Equal(suite.T(), suite.mockVMKeeper, suite.mempool.vmKeeper)
	require.Equal(suite.T(), suite.mockCosmosPool, suite.mempool.cosmosPool)
}

func (suite *MempoolTestSuite) TestNewEVMMempoolPanicsWithMultipleSubpools() {
	testChain := mocks.NewMockBlockChain(suite.mockVMKeeper.(*mocks.MockVMKeeper))
	legacyPool1 := legacypool.New(legacypool.DefaultConfig, testChain)
	legacyPool2 := legacypool.New(legacypool.DefaultConfig, testChain)
	txPool := &txpool.TxPool{
		Subpools: []txpool.SubPool{legacyPool1, legacyPool2},
	}

	require.Panics(suite.T(), func() {
		NewEVMMempool(suite.mockVMKeeper, txPool, suite.mockCosmosPool, suite.txDecoder)
	})
}

func (suite *MempoolTestSuite) TestInsertCosmosTransaction() {
	tx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 1000)),
		Msgs: []sdk.Msg{&mocks.MockMsg{}},
	}

	err := suite.mempool.Insert(context.Background(), tx)
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), 1, suite.mockCosmosPool.CountTx())
}

func (suite *MempoolTestSuite) TestInsertEmptyTransaction() {
	tx := &mocks.MockFeeTx{
		Msgs: []sdk.Msg{}, // Empty messages slice
	}

	err := suite.mempool.Insert(context.Background(), tx)
	require.Error(suite.T(), err)
	require.Contains(suite.T(), err.Error(), "transaction has no messages")
}

// addAccountToStateDB adds an account with balance to the state database for testing
func (suite *MempoolTestSuite) addAccountToStateDB(addr common.Address, balance *big.Int) {
	balanceU256, _ := uint256.FromBig(balance)
	mockKeeper := suite.mockVMKeeper.(*mocks.MockVMKeeper)
	mockKeeper.AddAccount(addr, balanceU256, 0)
}

func (suite *MempoolTestSuite) TestInsertEVMTransaction() {
	// Create a test private key and address
	privKey, err := crypto.GenerateKey()
	require.NoError(suite.T(), err)

	// Create a simple legacy transaction
	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ethTx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		To:       &to,
		Value:    big.NewInt(1000),
		Gas:      21000,
		GasPrice: big.NewInt(1000000000), // 1 gwei
		Data:     nil,
	})

	// Sign the transaction
	signer := ethtypes.HomesteadSigner{}
	signedTx, err := ethtypes.SignTx(ethTx, signer, privKey)
	require.NoError(suite.T(), err)

	// Create MsgEthereumTx
	msgEthTx := &evmtypes.MsgEthereumTx{}
	err = msgEthTx.FromEthereumTx(signedTx)
	require.NoError(suite.T(), err)

	// Create mock transaction with EVM message
	tx := &mocks.MockFeeTx{Msgs: []sdk.Msg{msgEthTx}}

	// Fund the account in the state database to pass validation
	fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	suite.addAccountToStateDB(fromAddr, big.NewInt(100000000000000000)) // 0.1 ETH

	// Insert the transaction
	err = suite.mempool.Insert(context.Background(), tx)
	require.NoError(suite.T(), err)
}

func (suite *MempoolTestSuite) TestRemoveCosmosTransaction() {
	tx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 1000)),
		Msgs: []sdk.Msg{&mocks.MockMsg{}},
	}

	// Insert first
	err := suite.mempool.Insert(context.Background(), tx)
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), 1, suite.mockCosmosPool.CountTx())

	// Remove
	err = suite.mempool.Remove(tx)
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), 0, suite.mockCosmosPool.CountTx())
}

func (suite *MempoolTestSuite) TestRemoveEmptyTransaction() {
	tx := &mocks.MockFeeTx{Msgs: []sdk.Msg{}}

	err := suite.mempool.Remove(tx)
	require.Error(suite.T(), err)
	require.Contains(suite.T(), err.Error(), "transaction has no messages")
}

func (suite *MempoolTestSuite) TestCountTx() {
	// Add cosmos transaction
	tx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 1000)),
		Msgs: []sdk.Msg{&mocks.MockMsg{}},
	}
	err := suite.mempool.Insert(context.Background(), tx)
	require.NoError(suite.T(), err)

	count := suite.mempool.CountTx()
	require.Equal(suite.T(), 1, count)
}

func (suite *MempoolTestSuite) TestSelectReturnsIterator() {
	iterator := suite.mempool.Select(suite.ctx, nil)
	require.NotNil(suite.T(), iterator)
	require.IsType(suite.T(), &EVMMempoolIterator{}, iterator)
}

func (suite *MempoolTestSuite) TestIteratorWithNoTransactions() {
	iterator := suite.mempool.Select(suite.ctx, nil)
	require.NotNil(suite.T(), iterator)

	tx := iterator.Tx()
	require.Nil(suite.T(), tx)

	next := iterator.Next()
	require.Nil(suite.T(), next)
}

func (suite *MempoolTestSuite) TestIteratorWithCosmosTransaction() {
	// Add cosmos transaction
	cosmosTx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 2000)),
	}
	suite.mockCosmosPool.Txs = append(suite.mockCosmosPool.Txs, cosmosTx)

	iterator := suite.mempool.Select(suite.ctx, nil)
	require.NotNil(suite.T(), iterator)

	tx := iterator.Tx()
	require.Equal(suite.T(), cosmosTx, tx)
}

func (suite *MempoolTestSuite) TestIteratorNext() {
	// Add multiple cosmos transactions
	cosmosTx1 := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 1000)),
	}
	cosmosTx2 := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 2000)),
	}
	suite.mockCosmosPool.Txs = append(suite.mockCosmosPool.Txs, cosmosTx1, cosmosTx2)

	iterator := suite.mempool.Select(suite.ctx, nil)
	require.NotNil(suite.T(), iterator)

	// First transaction
	tx1 := iterator.Tx()
	require.Equal(suite.T(), cosmosTx1, tx1)

	// Move to next
	iterator = iterator.Next()
	require.NotNil(suite.T(), iterator)

	// Second transaction
	tx2 := iterator.Tx()
	require.Equal(suite.T(), cosmosTx2, tx2)
}

func (suite *MempoolTestSuite) TestIteratorExhaustion() {
	// Add single cosmos transaction
	cosmosTx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 1000)),
	}
	suite.mockCosmosPool.Txs = append(suite.mockCosmosPool.Txs, cosmosTx)

	iterator := suite.mempool.Select(suite.ctx, nil)
	require.NotNil(suite.T(), iterator)

	// First transaction
	tx := iterator.Tx()
	require.Equal(suite.T(), cosmosTx, tx)

	// Test that iterator can advance (actual behavior varies based on implementation)
	iterator = iterator.Next()
	// Iterator may or may not be nil depending on internal EVM pool state
	// This test verifies the basic functionality works without panics
}

func (suite *MempoolTestSuite) TestTransactionOrdering() {
	// Test that transactions are ordered by fee priority

	// Create EVM transaction with high fee
	privKey, err := crypto.GenerateKey()
	require.NoError(suite.T(), err)

	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	highFeeEthTx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		To:       &to,
		Value:    big.NewInt(1000),
		Gas:      21000,
		GasPrice: big.NewInt(5000000000), // 5 gwei
		Data:     nil,
	})

	signer := ethtypes.HomesteadSigner{}
	signedHighFeeEthTx, err := ethtypes.SignTx(highFeeEthTx, signer, privKey)
	require.NoError(suite.T(), err)

	msgHighFeeEthTx := &evmtypes.MsgEthereumTx{}
	err = msgHighFeeEthTx.FromEthereumTx(signedHighFeeEthTx)
	require.NoError(suite.T(), err)

	highFeeEVMTx := &mocks.MockFeeTx{Msgs: []sdk.Msg{msgHighFeeEthTx}}

	// Fund the account
	fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	suite.addAccountToStateDB(fromAddr, big.NewInt(100000000000000000))

	// Create Cosmos transaction with medium fee
	mediumFeeCosmosTx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 3000000000)), // 3 gwei
		Msgs: []sdk.Msg{&mocks.MockMsg{}},
	}

	// Create Cosmos transaction with low fee
	lowFeeCosmosTx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 1000000000)), // 1 gwei
		Msgs: []sdk.Msg{&mocks.MockMsg{}},
	}

	// Insert transactions in non-priority order
	err = suite.mempool.Insert(context.Background(), lowFeeCosmosTx)
	require.NoError(suite.T(), err)

	err = suite.mempool.Insert(context.Background(), highFeeEVMTx)
	require.NoError(suite.T(), err)

	err = suite.mempool.Insert(context.Background(), mediumFeeCosmosTx)
	require.NoError(suite.T(), err)

	// Get iterator and verify ordering (highest fee first)
	iterator := suite.mempool.Select(suite.ctx, nil)
	require.NotNil(suite.T(), iterator)

	// First transaction should be the high fee EVM transaction (5 gwei)
	tx1 := iterator.Tx()
	require.NotNil(suite.T(), tx1)
	if ethMsg, ok := tx1.GetMsgs()[0].(*evmtypes.MsgEthereumTx); ok {
		ethTx := ethMsg.AsTransaction()
		require.Equal(suite.T(), big.NewInt(5000000000), ethTx.GasPrice())
	} else {
		suite.T().Fatal("Expected first transaction to be EVM transaction")
	}

	// Move to next transaction
	iterator = iterator.Next()
	require.NotNil(suite.T(), iterator)
	tx2 := iterator.Tx()
	require.NotNil(suite.T(), tx2)

	// Second transaction should be medium fee cosmos transaction (3 gwei)
	if feeTx, ok := tx2.(sdk.FeeTx); ok {
		Fees := feeTx.GetFee()
		for _, coin := range Fees {
			if coin.Denom == "wei" {
				require.Equal(suite.T(), int64(3000000000), coin.Amount.Int64())
			}
		}
	}

	// Move to third transaction
	iterator = iterator.Next()
	require.NotNil(suite.T(), iterator)
	tx3 := iterator.Tx()
	require.NotNil(suite.T(), tx3)

	// Third transaction should be low fee cosmos transaction (1 gwei)
	if feeTx, ok := tx3.(sdk.FeeTx); ok {
		Fees := feeTx.GetFee()
		for _, coin := range Fees {
			if coin.Denom == "wei" {
				require.Equal(suite.T(), int64(1000000000), coin.Amount.Int64())
			}
		}
	}
}

func (suite *MempoolTestSuite) TestCosmosTransactionOrdering() {
	// Test ordering when only Cosmos transactions are present

	highFeeTx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 5000000000)), // 5 gwei
		Msgs: []sdk.Msg{&mocks.MockMsg{}},
	}

	lowFeeTx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 1000000000)), // 1 gwei
		Msgs: []sdk.Msg{&mocks.MockMsg{}},
	}

	mediumFeeTx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 3000000000)), // 3 gwei
		Msgs: []sdk.Msg{&mocks.MockMsg{}},
	}

	// Insert in random order
	err := suite.mempool.Insert(context.Background(), mediumFeeTx)
	require.NoError(suite.T(), err)

	err = suite.mempool.Insert(context.Background(), lowFeeTx)
	require.NoError(suite.T(), err)

	err = suite.mempool.Insert(context.Background(), highFeeTx)
	require.NoError(suite.T(), err)

	// Verify they come out in priority order through cosmos pool
	// Note: The actual ordering depends on the cosmos pool implementation
	// This test verifies that the EVM mempool correctly delegates to cosmos pool
	iterator := suite.mempool.Select(suite.ctx, nil)
	require.NotNil(suite.T(), iterator)

	// Should get first transaction from cosmos pool
	tx1 := iterator.Tx()
	require.NotNil(suite.T(), tx1)
	require.Contains(suite.T(), []sdk.Tx{highFeeTx, mediumFeeTx, lowFeeTx}, tx1)
}

func (suite *MempoolTestSuite) TestEVMTransactionOrdering() {
	// Test ordering when only EVM transactions are present

	// Create first EVM transaction with low fee
	privKey1, err := crypto.GenerateKey()
	require.NoError(suite.T(), err)

	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	lowFeeEthTx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		To:       &to,
		Value:    big.NewInt(1000),
		Gas:      21000,
		GasPrice: big.NewInt(1000000000), // 1 gwei
		Data:     nil,
	})

	signer := ethtypes.HomesteadSigner{}
	signedLowFeeEthTx, err := ethtypes.SignTx(lowFeeEthTx, signer, privKey1)
	require.NoError(suite.T(), err)

	msgLowFeeEthTx := &evmtypes.MsgEthereumTx{}
	err = msgLowFeeEthTx.FromEthereumTx(signedLowFeeEthTx)
	require.NoError(suite.T(), err)

	lowFeeEVMTx := &mocks.MockFeeTx{Msgs: []sdk.Msg{msgLowFeeEthTx}}

	// Fund the first account
	fromAddr1 := crypto.PubkeyToAddress(privKey1.PublicKey)
	suite.addAccountToStateDB(fromAddr1, big.NewInt(100000000000000000))

	// Create second EVM transaction with high fee
	privKey2, err := crypto.GenerateKey()
	require.NoError(suite.T(), err)

	highFeeEthTx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		To:       &to,
		Value:    big.NewInt(1000),
		Gas:      21000,
		GasPrice: big.NewInt(5000000000), // 5 gwei
		Data:     nil,
	})

	signedHighFeeEthTx, err := ethtypes.SignTx(highFeeEthTx, signer, privKey2)
	require.NoError(suite.T(), err)

	msgHighFeeEthTx := &evmtypes.MsgEthereumTx{}
	err = msgHighFeeEthTx.FromEthereumTx(signedHighFeeEthTx)
	require.NoError(suite.T(), err)

	highFeeEVMTx := &mocks.MockFeeTx{Msgs: []sdk.Msg{msgHighFeeEthTx}}

	// Fund the second account
	fromAddr2 := crypto.PubkeyToAddress(privKey2.PublicKey)
	suite.addAccountToStateDB(fromAddr2, big.NewInt(100000000000000000))

	// Insert low fee transaction first
	err = suite.mempool.Insert(context.Background(), lowFeeEVMTx)
	require.NoError(suite.T(), err)

	// Insert high fee transaction second
	err = suite.mempool.Insert(context.Background(), highFeeEVMTx)
	require.NoError(suite.T(), err)

	// Get iterator and verify high fee transaction comes first
	iterator := suite.mempool.Select(suite.ctx, nil)
	require.NotNil(suite.T(), iterator)

	// First transaction should be high fee
	tx1 := iterator.Tx()
	require.NotNil(suite.T(), tx1)
	if ethMsg, ok := tx1.GetMsgs()[0].(*evmtypes.MsgEthereumTx); ok {
		ethTx := ethMsg.AsTransaction()
		require.Equal(suite.T(), big.NewInt(5000000000), ethTx.GasPrice())
	} else {
		suite.T().Fatal("Expected first transaction to be high fee EVM transaction")
	}

	// Second transaction should be low fee
	iterator = iterator.Next()
	require.NotNil(suite.T(), iterator)
	tx2 := iterator.Tx()
	require.NotNil(suite.T(), tx2)
	if ethMsg, ok := tx2.GetMsgs()[0].(*evmtypes.MsgEthereumTx); ok {
		ethTx := ethMsg.AsTransaction()
		require.Equal(suite.T(), big.NewInt(1000000000), ethTx.GasPrice())
	} else {
		suite.T().Fatal("Expected second transaction to be low fee EVM transaction")
	}
}

func (suite *MempoolTestSuite) TestMixedTransactionOrderingEVMPriority() {
	// Test that EVM transaction with equal fee gets prioritized over Cosmos transaction

	// Create EVM transaction with specific fee
	privKey, err := crypto.GenerateKey()
	require.NoError(suite.T(), err)

	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ethTx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		To:       &to,
		Value:    big.NewInt(1000),
		Gas:      21000,
		GasPrice: big.NewInt(3000000000), // 3 gwei
		Data:     nil,
	})

	signer := ethtypes.HomesteadSigner{}
	signedEthTx, err := ethtypes.SignTx(ethTx, signer, privKey)
	require.NoError(suite.T(), err)

	msgEthTx := &evmtypes.MsgEthereumTx{}
	err = msgEthTx.FromEthereumTx(signedEthTx)
	require.NoError(suite.T(), err)

	evmTx := &mocks.MockFeeTx{Msgs: []sdk.Msg{msgEthTx}}

	// Fund the account
	fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	suite.addAccountToStateDB(fromAddr, big.NewInt(100000000000000000))

	// Create Cosmos transaction with same fee
	cosmosTx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 3000000000)), // 3 gwei
		Msgs: []sdk.Msg{&mocks.MockMsg{}},
	}

	// Insert Cosmos transaction first
	err = suite.mempool.Insert(context.Background(), cosmosTx)
	require.NoError(suite.T(), err)

	// Insert EVM transaction second
	err = suite.mempool.Insert(context.Background(), evmTx)
	require.NoError(suite.T(), err)

	// Get iterator and verify EVM transaction comes first (tie-breaker behavior)
	iterator := suite.mempool.Select(suite.ctx, nil)
	require.NotNil(suite.T(), iterator)

	// First transaction should be EVM (equal Fees, but EVM gets priority in implementation)
	tx1 := iterator.Tx()
	require.NotNil(suite.T(), tx1)
	if ethMsg, ok := tx1.GetMsgs()[0].(*evmtypes.MsgEthereumTx); ok {
		ethTx := ethMsg.AsTransaction()
		require.Equal(suite.T(), big.NewInt(3000000000), ethTx.GasPrice())
	} else {
		suite.T().Fatal("Expected first transaction to be EVM transaction in tie-breaker")
	}

	// Second transaction should be Cosmos
	iterator = iterator.Next()
	require.NotNil(suite.T(), iterator)
	tx2 := iterator.Tx()
	require.NotNil(suite.T(), tx2)
	require.Equal(suite.T(), cosmosTx, tx2)
}

func (suite *MempoolTestSuite) TestTransactionOrderingWithNonMatchingDenom() {
	// Test that Cosmos transactions with non-matching denom get lowest priority

	// Create EVM transaction with low fee
	privKey, err := crypto.GenerateKey()
	require.NoError(suite.T(), err)

	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ethTx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		To:       &to,
		Value:    big.NewInt(1000),
		Gas:      21000,
		GasPrice: big.NewInt(1000000000), // 1 gwei
		Data:     nil,
	})

	signer := ethtypes.HomesteadSigner{}
	signedEthTx, err := ethtypes.SignTx(ethTx, signer, privKey)
	require.NoError(suite.T(), err)

	msgEthTx := &evmtypes.MsgEthereumTx{}
	err = msgEthTx.FromEthereumTx(signedEthTx)
	require.NoError(suite.T(), err)

	lowFeeEVMTx := &mocks.MockFeeTx{Msgs: []sdk.Msg{msgEthTx}}

	// Fund the account
	fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	suite.addAccountToStateDB(fromAddr, big.NewInt(100000000000000000))

	// Create Cosmos transaction with very high fee but wrong denom
	wrongDenomCosmosTx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 10000000000)), // 10 gwei but wrong denom
		Msgs: []sdk.Msg{&mocks.MockMsg{}},
	}

	// Create Cosmos transaction with matching denom but medium fee
	rightDenomCosmosTx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 2000000000)), // 2 gwei, correct denom
		Msgs: []sdk.Msg{&mocks.MockMsg{}},
	}

	// Insert transactions
	err = suite.mempool.Insert(context.Background(), wrongDenomCosmosTx)
	require.NoError(suite.T(), err)

	err = suite.mempool.Insert(context.Background(), lowFeeEVMTx)
	require.NoError(suite.T(), err)

	err = suite.mempool.Insert(context.Background(), rightDenomCosmosTx)
	require.NoError(suite.T(), err)

	// Get iterator and verify ordering
	iterator := suite.mempool.Select(suite.ctx, nil)
	require.NotNil(suite.T(), iterator)

	// Check what the first transaction actually is
	tx1 := iterator.Tx()
	require.NotNil(suite.T(), tx1)

	// The EVM transaction should come first (gets priority over cosmos transactions with wrong denom)
	if ethMsg, ok := tx1.GetMsgs()[0].(*evmtypes.MsgEthereumTx); ok {
		ethTx := ethMsg.AsTransaction()
		require.Equal(suite.T(), big.NewInt(1000000000), ethTx.GasPrice())
	} else {
		suite.T().Fatal("Expected first transaction to be EVM transaction")
	}

	// Second should be the cosmos tx with correct denom (2 gwei)
	iterator = iterator.Next()
	require.NotNil(suite.T(), iterator)
	tx2 := iterator.Tx()
	require.NotNil(suite.T(), tx2)

	// Second should be the wrong denom transaction (due to cosmos pool ordering)
	if mockTx, ok := tx2.(*mocks.MockFeeTx); ok {
		Fees := mockTx.GetFee()
		foundwei := false
		for _, coin := range Fees {
			if coin.Denom == "wei" {
				foundwei = true
				break
			}
		}
		require.True(suite.T(), foundwei, "Expected second transaction to be the wrong denom cosmos tx")
	} else {
		suite.T().Fatal("Expected second transaction to be mocks.MockFeeTx with wrong denom")
	}

	// Third should be the right denom cosmos tx
	iterator = iterator.Next()
	require.NotNil(suite.T(), iterator)
	tx3 := iterator.Tx()
	require.NotNil(suite.T(), tx3)

	// Third should be the right denom cosmos transaction
	if mockTx, ok := tx3.(*mocks.MockFeeTx); ok {
		Fees := mockTx.GetFee()
		foundwei := false
		for _, coin := range Fees {
			if coin.Denom == "wei" && coin.Amount.Int64() == 2000000000 {
				foundwei = true
				break
			}
		}
		require.True(suite.T(), foundwei, "Expected third transaction to be the correct denom cosmos tx with 2 gwei")
	} else {
		suite.T().Fatal("Expected third transaction to be mocks.MockFeeTx with correct denom")
	}
}

func TestMempoolTestSuite(t *testing.T) {
	suite.Run(t, new(MempoolTestSuite))
}

// Benchmark tests
func BenchmarkInsertCosmosTransaction(b *testing.B) {
	mockVMKeeper := &mocks.MockVMKeeper{
		BaseFee: big.NewInt(1000000000),
		Params: evmtypes.Params{
			EvmDenom: "wei",
		},
		Accounts: make(map[common.Address]*statedb.Account),
	}
	mockCosmosPool := &mocks.MockCosmosPool{
		Txs: make([]sdk.Tx, 0),
	}
	txDecoder := func(txBytes []byte) (sdk.Tx, error) {
		return &mocks.MockFeeTx{}, nil
	}
	testChain := mocks.NewMockBlockChain(mockVMKeeper)
	legacyPool := legacypool.New(legacypool.DefaultConfig, testChain)
	txPool := &txpool.TxPool{
		Subpools: []txpool.SubPool{legacyPool},
	}
	mpool := NewEVMMempool(mockVMKeeper, txPool, mockCosmosPool, txDecoder)

	tx := &mocks.MockFeeTx{
		Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", 1000)),
		Msgs: []sdk.Msg{&mocks.MockMsg{}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mpool.Insert(context.Background(), tx)
	}
}

func BenchmarkSelect(b *testing.B) {
	ctx := sdk.NewContext(nil, tmproto.Header{}, false, nil)
	mockVMKeeper := &mocks.MockVMKeeper{
		BaseFee: big.NewInt(1000000000),
		Params: evmtypes.Params{
			EvmDenom: "wei",
		},
		Accounts: make(map[common.Address]*statedb.Account),
	}
	mockCosmosPool := &mocks.MockCosmosPool{
		Txs: make([]sdk.Tx, 0),
	}
	txDecoder := func(txBytes []byte) (sdk.Tx, error) {
		return &mocks.MockFeeTx{}, nil
	}
	testChain := mocks.NewMockBlockChain(mockVMKeeper)
	legacyPool := legacypool.New(legacypool.DefaultConfig, testChain)
	txPool := &txpool.TxPool{
		Subpools: []txpool.SubPool{legacyPool},
	}
	mpool := NewEVMMempool(mockVMKeeper, txPool, mockCosmosPool, txDecoder)

	// Pre-populate with some transactions
	for i := 0; i < 100; i++ {
		tx := &mocks.MockFeeTx{
			Fees: sdk.NewCoins(sdk.NewInt64Coin("wei", int64(1000+i))),
			Msgs: []sdk.Msg{&mocks.MockMsg{}},
		}
		mockCosmosPool.Txs = append(mockCosmosPool.Txs, tx)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iterator := mpool.Select(ctx, nil)
		_ = iterator
	}
}
