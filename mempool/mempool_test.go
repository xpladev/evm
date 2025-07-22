package mempool

import (
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	testutil2 "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/evm/encoding"
	utiltx "github.com/cosmos/evm/testutil/tx"

	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmosMempool "github.com/cosmos/cosmos-sdk/types/mempool"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/evm/mempool/mocks"
	"github.com/cosmos/evm/mempool/txpool"
	"github.com/cosmos/evm/mempool/txpool/legacypool"
	testconstants "github.com/cosmos/evm/testutil/constants"
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
	cosmosPool     cosmosMempool.ExtMempool
	txDecoder      sdk.TxDecoder
	mockChain      *mocks.MockBlockChain
	encodingConfig testutil2.TestEncodingConfig
}

func (suite *MempoolTestSuite) SetupTest() {
	// Create a proper context with a memory store
	db := dbm.NewMemDB()
	storeKey := storetypes.NewKVStoreKey("test")
	cms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	_ = cms.LoadLatestVersion()

	suite.ctx = sdk.NewContext(cms, cmtproto.Header{}, false, log.NewNopLogger())

	// Initialize encoding config
	suite.encodingConfig = encoding.MakeConfig(testconstants.ExampleChainID.EVMChainID)

	suite.mockVMKeeper = &mocks.MockVMKeeper{
		BaseFee: big.NewInt(1000000000), // 1 gwei
		Params: evmtypes.Params{
			EvmDenom: "wei",
		},
		Accounts: make(map[common.Address]*statedb.Account),
	}

	// Create a PriorityNonceMempool as the cosmosPool
	suite.cosmosPool = cosmosMempool.DefaultPriorityMempool()

	// Create a real txDecoder using the encoding config
	suite.txDecoder = suite.encodingConfig.TxConfig.TxDecoder()

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

	suite.mempool = NewEVMMempool(suite.mockVMKeeper, txPool, suite.cosmosPool, suite.txDecoder)
}

// Test helper functions
func (suite *MempoolTestSuite) addAccountToStateDB(addr common.Address, balance *big.Int) {
	balanceU256, _ := uint256.FromBig(balance)
	mockKeeper := suite.mockVMKeeper.(*mocks.MockVMKeeper)
	mockKeeper.AddAccount(addr, balanceU256, 0)
}

func (suite *MempoolTestSuite) createEVMTransaction(gasPrice *big.Int) (sdk.Tx, *ecdsa.PrivateKey, error) {
	privKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, nil, err
	}

	to := common.HexToAddress("0x1234567890123456789012345678901234567890")
	ethTx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		To:       &to,
		Value:    big.NewInt(1000),
		Gas:      21000,
		GasPrice: gasPrice,
		Data:     nil,
	})

	signer := ethtypes.HomesteadSigner{}
	signedTx, err := ethtypes.SignTx(ethTx, signer, privKey)
	if err != nil {
		return nil, nil, err
	}

	msgEthTx := &evmtypes.MsgEthereumTx{}
	err = msgEthTx.FromEthereumTx(signedTx)
	if err != nil {
		return nil, nil, err
	}

	// Create a real transaction using txbuilder
	txBuilder := suite.encodingConfig.TxConfig.NewTxBuilder()
	err = txBuilder.SetMsgs(msgEthTx)
	if err != nil {
		return nil, nil, err
	}

	return txBuilder.GetTx(), privKey, nil
}

func (suite *MempoolTestSuite) createCosmosTransaction(feeDenom string, feeAmount int64) sdk.Tx {
	// Create a simple bank send message
	fromAddr := sdk.AccAddress("test_from_address__")
	toAddr := sdk.AccAddress("test_to_address____")
	amount := sdk.NewCoins(sdk.NewInt64Coin(feeDenom, 1000))

	bankMsg := banktypes.NewMsgSend(fromAddr, toAddr, amount)

	// Create a real transaction using txbuilder
	txBuilder := suite.encodingConfig.TxConfig.NewTxBuilder()
	err := txBuilder.SetMsgs(bankMsg)
	if err != nil {
		suite.T().Fatalf("Failed to set messages: %v", err)
	}
	// Create signature unrelated to payload for testing
	signatureHex := strings.Repeat("01", 65)
	signatureBytes, err := hex.DecodeString(signatureHex)
	require.NoError(suite.T(), err)
	_, privKey := utiltx.NewAddrKey()
	sigsV2 := signing.SignatureV2{
		PubKey: privKey.PubKey(), // Use unrelated public key for testing
		Data: &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: signatureBytes,
		},
		Sequence: 0,
	}
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(feeDenom, feeAmount)))
	err = txBuilder.SetSignatures(sigsV2)
	require.NoError(suite.T(), err)
	txBuilder.SetGasLimit(200000) // Set a reasonable gas limit

	return txBuilder.GetTx()
}

func (suite *MempoolTestSuite) TestNewEVMMempool() {
	tests := []struct {
		name      string
		setup     func() (*txpool.TxPool, bool)
		wantPanic bool
	}{
		{
			name: "valid single subpool",
			setup: func() (*txpool.TxPool, bool) {
				testChain := mocks.NewMockBlockChain(suite.mockVMKeeper.(*mocks.MockVMKeeper))
				legacyPool := legacypool.New(legacypool.DefaultConfig, testChain)
				return &txpool.TxPool{Subpools: []txpool.SubPool{legacyPool}}, false
			},
			wantPanic: false,
		},
		{
			name: "multiple subpools should panic",
			setup: func() (*txpool.TxPool, bool) {
				testChain := mocks.NewMockBlockChain(suite.mockVMKeeper.(*mocks.MockVMKeeper))
				legacyPool1 := legacypool.New(legacypool.DefaultConfig, testChain)
				legacyPool2 := legacypool.New(legacypool.DefaultConfig, testChain)
				return &txpool.TxPool{Subpools: []txpool.SubPool{legacyPool1, legacyPool2}}, true
			},
			wantPanic: true,
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			txPool, _ := tc.setup()
			if tc.wantPanic {
				require.Panics(t, func() {
					NewEVMMempool(suite.mockVMKeeper, txPool, suite.cosmosPool, suite.txDecoder)
				})
			} else {
				mempoolInstance := NewEVMMempool(suite.mockVMKeeper, txPool, suite.cosmosPool, suite.txDecoder)
				require.NotNil(t, mempoolInstance)
				require.Equal(t, suite.mockVMKeeper, mempoolInstance.vmKeeper)
				require.Equal(t, suite.cosmosPool, mempoolInstance.cosmosPool)
			}
		})
	}
}

func (suite *MempoolTestSuite) TestInsert() {
	tests := []struct {
		name          string
		setupTx       func() sdk.Tx
		setupAccount  func() // Setup any required accounts
		wantError     bool
		errorContains string
		verifyFunc    func(t *testing.T) // Additional verification
	}{
		{
			name: "cosmos transaction success",
			setupTx: func() sdk.Tx {
				return suite.createCosmosTransaction("wei", 1000)
			},
			setupAccount: func() {},
			wantError:    false,
			verifyFunc: func(t *testing.T) {
				require.Equal(t, 1, suite.cosmosPool.CountTx())
			},
		},
		{
			name: "empty transaction should fail",
			setupTx: func() sdk.Tx {
				// Create a transaction with no messages using txbuilder
				txBuilder := suite.encodingConfig.TxConfig.NewTxBuilder()
				return txBuilder.GetTx()
			},
			setupAccount:  func() {},
			wantError:     true,
			errorContains: "transaction has no messages",
			verifyFunc:    func(t *testing.T) {},
		},
		{
			name: "EVM transaction success",
			setupTx: func() sdk.Tx {
				tx, privKey, err := suite.createEVMTransaction(big.NewInt(1000000000))
				require.NoError(suite.T(), err)
				// Fund the account
				fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
				suite.addAccountToStateDB(fromAddr, big.NewInt(100000000000000000))
				return tx
			},
			setupAccount: func() {},
			wantError:    false,
			verifyFunc: func(t *testing.T) {
				p, _ := suite.mempool.txPool.Stats()
				require.Equal(t, 1, p)
			},
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Reset state for each test by creating a new cosmos pool
			suite.cosmosPool = cosmosMempool.DefaultPriorityMempool()
			suite.mempool = NewEVMMempool(suite.mockVMKeeper, suite.mempool.txPool, suite.cosmosPool, suite.txDecoder)

			tc.setupAccount()
			tx := tc.setupTx()

			err := suite.mempool.Insert(suite.ctx, tx)

			if tc.wantError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)
			}

			tc.verifyFunc(t)
		})
	}
}

func (suite *MempoolTestSuite) TestRemove() {
	tests := []struct {
		name          string
		setupTx       func() sdk.Tx
		setupAccount  func()
		insertFirst   bool
		wantError     bool
		errorContains string
		verifyFunc    func(t *testing.T)
	}{
		{
			name: "remove cosmos transaction success",
			setupTx: func() sdk.Tx {
				return suite.createCosmosTransaction("wei", 1000)
			},
			setupAccount: func() {},
			insertFirst:  true,
			wantError:    false,
			verifyFunc: func(t *testing.T) {
				require.Equal(t, 0, suite.cosmosPool.CountTx())
			},
		},
		{
			name: "remove empty transaction should fail",
			setupTx: func() sdk.Tx {
				// Create a transaction with no messages using txbuilder
				txBuilder := suite.encodingConfig.TxConfig.NewTxBuilder()
				return txBuilder.GetTx()
			},
			setupAccount:  func() {},
			insertFirst:   false,
			wantError:     true,
			errorContains: "transaction has no messages",
			verifyFunc:    func(t *testing.T) {},
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Reset state for each test by creating a new cosmos pool
			suite.cosmosPool = cosmosMempool.DefaultPriorityMempool()
			suite.mempool = NewEVMMempool(suite.mockVMKeeper, suite.mempool.txPool, suite.cosmosPool, suite.txDecoder)

			tc.setupAccount()
			tx := tc.setupTx()

			if tc.insertFirst {
				err := suite.mempool.Insert(suite.ctx, tx)
				require.NoError(t, err)
				require.Equal(t, 1, suite.cosmosPool.CountTx())
			}

			err := suite.mempool.Remove(tx)

			if tc.wantError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(t, err)
			}

			tc.verifyFunc(t)
		})
	}
}

func (suite *MempoolTestSuite) TestSelect() {
	tests := []struct {
		name       string
		setupTxs   func()
		verifyFunc func(t *testing.T, iterator cosmosMempool.Iterator)
	}{
		{
			name:     "empty mempool returns iterator",
			setupTxs: func() {},
			verifyFunc: func(t *testing.T, iterator cosmosMempool.Iterator) {
				require.NotNil(t, iterator)
				require.IsType(t, &EVMMempoolIterator{}, iterator)
			},
		},
		{
			name: "single cosmos transaction",
			setupTxs: func() {
				cosmosTx := suite.createCosmosTransaction("wei", 2000)
				err := suite.cosmosPool.Insert(suite.ctx, cosmosTx)
				require.NoError(suite.T(), err)
			},
			verifyFunc: func(t *testing.T, iterator cosmosMempool.Iterator) {
				require.NotNil(t, iterator)
				tx := iterator.Tx()
				require.NotNil(t, tx)
			},
		},
		{
			name: "count transactions",
			setupTxs: func() {
				tx := suite.createCosmosTransaction("wei", 1000)
				err := suite.mempool.Insert(suite.ctx, tx)
				require.NoError(suite.T(), err)
			},
			verifyFunc: func(t *testing.T, iterator cosmosMempool.Iterator) {
				count := suite.mempool.CountTx()
				require.Equal(t, 1, count)
			},
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Reset state for each test by creating a new cosmos pool
			suite.cosmosPool = cosmosMempool.DefaultPriorityMempool()
			suite.mempool = NewEVMMempool(suite.mockVMKeeper, suite.mempool.txPool, suite.cosmosPool, suite.txDecoder)
			tc.setupTxs()

			iterator := suite.mempool.Select(suite.ctx, nil)
			tc.verifyFunc(t, iterator)
		})
	}
}

func (suite *MempoolTestSuite) TestIterator() {
	tests := []struct {
		name       string
		setupTxs   func()
		verifyFunc func(t *testing.T, iterator cosmosMempool.Iterator)
	}{
		{
			name:     "empty iterator",
			setupTxs: func() {},
			verifyFunc: func(t *testing.T, iterator cosmosMempool.Iterator) {
				tx := iterator.Tx()
				require.Nil(t, tx)
				next := iterator.Next()
				require.Nil(t, next)
			},
		},
		{
			name: "single cosmos transaction iteration",
			setupTxs: func() {
				cosmosTx := suite.createCosmosTransaction("wei", 2000)
				err := suite.cosmosPool.Insert(suite.ctx, cosmosTx)
				require.NoError(suite.T(), err)
			},
			verifyFunc: func(t *testing.T, iterator cosmosMempool.Iterator) {
				tx := iterator.Tx()
				require.NotNil(t, tx)
			},
		},
		{
			name: "multiple cosmos transactions iteration",
			setupTxs: func() {
				cosmosTx1 := suite.createCosmosTransaction("wei", 1000)
				cosmosTx2 := suite.createCosmosTransaction("wei", 2000)
				err := suite.cosmosPool.Insert(suite.ctx, cosmosTx1)
				require.NoError(suite.T(), err)
				err = suite.cosmosPool.Insert(suite.ctx, cosmosTx2)
				require.NoError(suite.T(), err)
			},
			verifyFunc: func(t *testing.T, iterator cosmosMempool.Iterator) {
				// First transaction
				tx1 := iterator.Tx()
				require.NotNil(t, tx1)

				// Move to next
				iterator = iterator.Next()
				require.NotNil(t, iterator)

				// Second transaction
				tx2 := iterator.Tx()
				require.NotNil(t, tx2)
			},
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Reset state for each test by creating a new cosmos pool
			suite.cosmosPool = cosmosMempool.DefaultPriorityMempool()
			suite.mempool = NewEVMMempool(suite.mockVMKeeper, suite.mempool.txPool, suite.cosmosPool, suite.txDecoder)
			tc.setupTxs()

			iterator := suite.mempool.Select(suite.ctx, nil)
			tc.verifyFunc(t, iterator)
		})
	}
}

func (suite *MempoolTestSuite) TestTransactionOrdering() {
	tests := []struct {
		name       string
		setupTxs   func()
		verifyFunc func(t *testing.T, iterator cosmosMempool.Iterator)
	}{
		{
			name: "mixed EVM and cosmos transaction ordering",
			setupTxs: func() {
				// Create EVM transaction with high fee
				highFeeEVMTx, privKey, err := suite.createEVMTransaction(big.NewInt(5000000000)) // 5 gwei
				require.NoError(suite.T(), err)
				fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
				suite.addAccountToStateDB(fromAddr, big.NewInt(100000000000000000))

				// Create Cosmos transactions with medium and low fees
				mediumFeeCosmosTx := suite.createCosmosTransaction("wei", 3000000000) // 3 gwei
				lowFeeCosmosTx := suite.createCosmosTransaction("wei", 1000000000)    // 1 gwei

				// Insert in non-priority order
				err = suite.mempool.Insert(suite.ctx, lowFeeCosmosTx)
				require.NoError(suite.T(), err)
				err = suite.mempool.Insert(suite.ctx, highFeeEVMTx)
				require.NoError(suite.T(), err)
				err = suite.mempool.Insert(suite.ctx, mediumFeeCosmosTx)
				require.NoError(suite.T(), err)
			},
			verifyFunc: func(t *testing.T, iterator cosmosMempool.Iterator) {
				// First transaction should be the high fee EVM transaction (5 gwei)
				tx1 := iterator.Tx()
				require.NotNil(t, tx1)
				if ethMsg, ok := tx1.GetMsgs()[0].(*evmtypes.MsgEthereumTx); ok {
					ethTx := ethMsg.AsTransaction()
					require.Equal(t, big.NewInt(5000000000), ethTx.GasPrice())
				} else {
					t.Fatal("Expected first transaction to be EVM transaction")
				}
			},
		},
		{
			name: "EVM transaction priority over cosmos with equal fee",
			setupTxs: func() {
				// Create EVM transaction  
				evmTx, privKey, err := suite.createEVMTransaction(big.NewInt(3000000000)) // 3 gwei
				require.NoError(suite.T(), err)
				fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
				suite.addAccountToStateDB(fromAddr, big.NewInt(100000000000000000))

				// Create Cosmos transaction with same fee as what EVM iterator reports (2 gwei)
				// Note: EVM iterator returns 2000000000 despite gas price being 3000000000
				cosmosTx := suite.createCosmosTransaction("wei", 2000000000)

				// Insert Cosmos transaction first
				err = suite.mempool.Insert(suite.ctx, cosmosTx)
				require.NoError(suite.T(), err)
				err = suite.mempool.Insert(suite.ctx, evmTx)
				require.NoError(suite.T(), err)
				
			},
			verifyFunc: func(t *testing.T, iterator cosmosMempool.Iterator) {
				// First transaction should be EVM (tie-breaker behavior)
				tx1 := iterator.Tx()
				require.NotNil(t, tx1)
				if ethMsg, ok := tx1.GetMsgs()[0].(*evmtypes.MsgEthereumTx); ok {
					ethTx := ethMsg.AsTransaction()
					require.Equal(t, big.NewInt(3000000000), ethTx.GasPrice())
				} else {
					t.Fatal("Expected first transaction to be EVM transaction in tie-breaker")
				}
			},
		},
		{
			name: "EVM-only transaction ordering",
			setupTxs: func() {
				// Create first EVM transaction with low fee
				lowFeeEVMTx, privKey1, err := suite.createEVMTransaction(big.NewInt(1000000000)) // 1 gwei
				require.NoError(suite.T(), err)
				fromAddr1 := crypto.PubkeyToAddress(privKey1.PublicKey)
				suite.addAccountToStateDB(fromAddr1, big.NewInt(100000000000000000))

				// Create second EVM transaction with high fee
				highFeeEVMTx, privKey2, err := suite.createEVMTransaction(big.NewInt(5000000000)) // 5 gwei
				require.NoError(suite.T(), err)
				fromAddr2 := crypto.PubkeyToAddress(privKey2.PublicKey)
				suite.addAccountToStateDB(fromAddr2, big.NewInt(100000000000000000))

				// Insert low fee transaction first
				err = suite.mempool.Insert(suite.ctx, lowFeeEVMTx)
				require.NoError(suite.T(), err)
				err = suite.mempool.Insert(suite.ctx, highFeeEVMTx)
				require.NoError(suite.T(), err)
			},
			verifyFunc: func(t *testing.T, iterator cosmosMempool.Iterator) {
				// First transaction should be high fee
				tx1 := iterator.Tx()
				require.NotNil(t, tx1)
				if ethMsg, ok := tx1.GetMsgs()[0].(*evmtypes.MsgEthereumTx); ok {
					ethTx := ethMsg.AsTransaction()
					require.Equal(t, big.NewInt(5000000000), ethTx.GasPrice())
				} else {
					t.Fatal("Expected first transaction to be high fee EVM transaction")
				}
			},
		},
		{
			name: "cosmos-only transaction ordering",
			setupTxs: func() {
				highFeeTx := suite.createCosmosTransaction("wei", 5000000000)   // 5 gwei
				lowFeeTx := suite.createCosmosTransaction("wei", 1000000000)    // 1 gwei
				mediumFeeTx := suite.createCosmosTransaction("wei", 3000000000) // 3 gwei

				// Insert in random order
				err := suite.mempool.Insert(suite.ctx, mediumFeeTx)
				require.NoError(suite.T(), err)
				err = suite.mempool.Insert(suite.ctx, lowFeeTx)
				require.NoError(suite.T(), err)
				err = suite.mempool.Insert(suite.ctx, highFeeTx)
				require.NoError(suite.T(), err)
			},
			verifyFunc: func(t *testing.T, iterator cosmosMempool.Iterator) {
				// Should get first transaction from cosmos pool
				tx1 := iterator.Tx()
				require.NotNil(t, tx1)
				// Note: The actual ordering depends on the cosmos pool implementation
			},
		},
		{
			name: "wrong denomination handling",
			setupTxs: func() {
				// Create EVM transaction with low fee
				lowFeeEVMTx, privKey, err := suite.createEVMTransaction(big.NewInt(2000000000)) // 2 gwei
				require.NoError(suite.T(), err)
				fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
				suite.addAccountToStateDB(fromAddr, big.NewInt(100000000000000000))

				// Create Cosmos transaction with high fee but wrong denom  
				wrongDenomCosmosTx := suite.createCosmosTransaction("uatom", 10000000000) // 10 gwei but wrong denom
				rightDenomCosmosTx := suite.createCosmosTransaction("wei", 1000000000)   // 1 gwei correct denom

				// Insert transactions
				err = suite.mempool.Insert(suite.ctx, wrongDenomCosmosTx)
				require.NoError(suite.T(), err)
				err = suite.mempool.Insert(suite.ctx, lowFeeEVMTx)
				require.NoError(suite.T(), err)
				err = suite.mempool.Insert(suite.ctx, rightDenomCosmosTx)
				require.NoError(suite.T(), err)
				
			},
			verifyFunc: func(t *testing.T, iterator cosmosMempool.Iterator) {
				// The EVM transaction should come first
				tx1 := iterator.Tx()
				require.NotNil(t, tx1)
				if ethMsg, ok := tx1.GetMsgs()[0].(*evmtypes.MsgEthereumTx); ok {
					ethTx := ethMsg.AsTransaction()
					require.Equal(t, big.NewInt(2000000000), ethTx.GasPrice())
				} else {
					t.Fatal("Expected first transaction to be EVM transaction")
				}
			},
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Reset state for each test by creating new pools
			suite.cosmosPool = cosmosMempool.DefaultPriorityMempool()
			
			// Create a fresh EVM pool
			suite.mockChain = mocks.NewMockBlockChain(suite.mockVMKeeper.(*mocks.MockVMKeeper))
			legacyPool := legacypool.New(legacypool.DefaultConfig, suite.mockChain)
			reserver := &mocks.MockReserver{}
			err := legacyPool.Init(1000000000, suite.mockChain.CurrentBlock(), reserver)
			require.NoError(suite.T(), err)
			txPool := &txpool.TxPool{
				Subpools: []txpool.SubPool{legacyPool},
			}
			
			suite.mempool = NewEVMMempool(suite.mockVMKeeper, txPool, suite.cosmosPool, suite.txDecoder)
			tc.setupTxs()

			iterator := suite.mempool.Select(suite.ctx, nil)
			tc.verifyFunc(t, iterator)
		})
	}
}

func TestMempoolTestSuite(t *testing.T) {
	suite.Run(t, new(MempoolTestSuite))
}

// Benchmark tests
func BenchmarkInsertCosmosTransaction(b *testing.B) {
	encodingConfig := encoding.MakeConfig(testconstants.ExampleChainID.EVMChainID)
	mockVMKeeper := &mocks.MockVMKeeper{
		BaseFee: big.NewInt(1000000000),
		Params: evmtypes.Params{
			EvmDenom: "wei",
		},
		Accounts: make(map[common.Address]*statedb.Account),
	}
	cosmosPool := cosmosMempool.DefaultPriorityMempool()
	txDecoder := encodingConfig.TxConfig.TxDecoder()
	testChain := mocks.NewMockBlockChain(mockVMKeeper)
	legacyPool := legacypool.New(legacypool.DefaultConfig, testChain)
	txPool := &txpool.TxPool{
		Subpools: []txpool.SubPool{legacyPool},
	}
	mpool := NewEVMMempool(mockVMKeeper, txPool, cosmosPool, txDecoder)

	// Create a real bank message transaction
	fromAddr := sdk.AccAddress("test_from_address__")
	toAddr := sdk.AccAddress("test_to_address____")
	amount := sdk.NewCoins(sdk.NewInt64Coin("wei", 1000))
	bankMsg := banktypes.NewMsgSend(fromAddr, toAddr, amount)

	txBuilder := encodingConfig.TxConfig.NewTxBuilder()
	_ = txBuilder.SetMsgs(bankMsg)
	txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("wei", 1000)))
	txBuilder.SetGasLimit(200000)
	tx := txBuilder.GetTx()

	// Create context for benchmark
	db2 := dbm.NewMemDB()
	storeKey2 := storetypes.NewKVStoreKey("test2")
	cms2 := store.NewCommitMultiStore(db2, log.NewNopLogger(), metrics.NewNoOpMetrics())
	cms2.MountStoreWithDB(storeKey2, storetypes.StoreTypeIAVL, db2)
	_ = cms2.LoadLatestVersion()
	benchCtx := sdk.NewContext(cms2, cmtproto.Header{}, false, log.NewNopLogger())
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mpool.Insert(benchCtx, tx)
	}
}

func BenchmarkSelect(b *testing.B) {
	// Create a proper context with a memory store
	db := dbm.NewMemDB()
	storeKey := storetypes.NewKVStoreKey("test")
	cms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	_ = cms.LoadLatestVersion()

	ctx := sdk.NewContext(cms, cmtproto.Header{}, false, log.NewNopLogger())
	encodingConfig := encoding.MakeConfig(testconstants.ExampleChainID.EVMChainID)
	mockVMKeeper := &mocks.MockVMKeeper{
		BaseFee: big.NewInt(1000000000),
		Params: evmtypes.Params{
			EvmDenom: "wei",
		},
		Accounts: make(map[common.Address]*statedb.Account),
	}
	cosmosPool := cosmosMempool.DefaultPriorityMempool()
	txDecoder := encodingConfig.TxConfig.TxDecoder()
	testChain := mocks.NewMockBlockChain(mockVMKeeper)
	legacyPool := legacypool.New(legacypool.DefaultConfig, testChain)
	txPool := &txpool.TxPool{
		Subpools: []txpool.SubPool{legacyPool},
	}
	mpool := NewEVMMempool(mockVMKeeper, txPool, cosmosPool, txDecoder)

	// Pre-populate with some transactions
	for i := 0; i < 100; i++ {
		// Create a real bank message transaction
		fromAddr := sdk.AccAddress("test_from_address__")
		toAddr := sdk.AccAddress("test_to_address____")
		amount := sdk.NewCoins(sdk.NewInt64Coin("wei", 1000))
		bankMsg := banktypes.NewMsgSend(fromAddr, toAddr, amount)

		txBuilder := encodingConfig.TxConfig.NewTxBuilder()
		_ = txBuilder.SetMsgs(bankMsg)
		txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("wei", int64(1000+i))))
		txBuilder.SetGasLimit(200000)
		tx := txBuilder.GetTx()

		_ = cosmosPool.Insert(ctx, tx)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iterator := mpool.Select(ctx, nil)
		_ = iterator
	}
}
