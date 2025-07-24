package mempool

import (
	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"crypto/ecdsa"
	"encoding/hex"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	testutil2 "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/evm/encoding"
	utiltx "github.com/cosmos/evm/testutil/tx"
	"math/big"
	"strings"
	"testing"

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
	ctx                 sdk.Context
	mempool             *EVMMempool
	mockVMKeeper        *mocks.MockVMKeeper
	mockFeeMarketKeeper *mocks.MockFeeMarketKeeper
	cosmosPool          cosmosMempool.ExtMempool
	txDecoder           sdk.TxDecoder
	mockChain           *mocks.MockBlockChain
	encodingConfig      testutil2.TestEncodingConfig
	ctxFunc             func(height int64, prove bool) (sdk.Context, error)
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

	suite.mockFeeMarketKeeper = &mocks.MockFeeMarketKeeper{
		BlockGasWanted: 1000000, // 1M gas
	}

	// Create a PriorityNonceMempool as the cosmosPool
	suite.cosmosPool = cosmosMempool.DefaultPriorityMempool()

	// Create a real txDecoder using the encoding config
	suite.txDecoder = suite.encodingConfig.TxConfig.TxDecoder()

	// Create a minimal txpool with legacypool
	suite.mockChain = mocks.NewMockBlockChain(suite.mockVMKeeper)
	legacyPool := legacypool.New(legacypool.DefaultConfig, suite.mockChain)

	// Initialize the legacy pool with a proper header
	reserver := &mocks.MockReserver{}
	err := legacyPool.Init(1000000000, suite.mockChain.CurrentBlock(), reserver)
	require.NoError(suite.T(), err)

	txPool := &txpool.TxPool{
		Subpools: []txpool.SubPool{legacyPool},
	}

	suite.ctxFunc = func(height int64, prove bool) (sdk.Context, error) {
		return suite.ctx, nil
	}
	suite.mempool = NewEVMMempool(suite.ctxFunc, suite.mockVMKeeper, suite.mockFeeMarketKeeper, suite.txDecoder, &EVMMempoolConfig{
		TxPool:     txPool,
		CosmosPool: suite.cosmosPool,
	})
}

// Test helper functions
func (suite *MempoolTestSuite) addAccountToStateDB(addr common.Address, balance *big.Int) {
	balanceU256, _ := uint256.FromBig(balance)
	mockKeeper := suite.mockVMKeeper
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
				testChain := mocks.NewMockBlockChain(suite.mockVMKeeper)
				legacyPool := legacypool.New(legacypool.DefaultConfig, testChain)
				return &txpool.TxPool{Subpools: []txpool.SubPool{legacyPool}}, false
			},
			wantPanic: false,
		},
		{
			name: "multiple subpools should panic",
			setup: func() (*txpool.TxPool, bool) {
				testChain := mocks.NewMockBlockChain(suite.mockVMKeeper)
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
					ctxFunc := func(height int64, prove bool) (sdk.Context, error) {
						return suite.ctx, nil
					}
					NewEVMMempool(ctxFunc, suite.mockVMKeeper, suite.mockFeeMarketKeeper, suite.txDecoder, &EVMMempoolConfig{
						TxPool:     txPool,
						CosmosPool: suite.cosmosPool,
					})
				})
			} else {
				ctxFunc := func(height int64, prove bool) (sdk.Context, error) {
					return suite.ctx, nil
				}
				mempoolInstance := NewEVMMempool(ctxFunc, suite.mockVMKeeper, suite.mockFeeMarketKeeper, suite.txDecoder, &EVMMempoolConfig{
					TxPool:     txPool,
					CosmosPool: suite.cosmosPool,
				})
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
			suite.mempool = NewEVMMempool(suite.ctxFunc, suite.mockVMKeeper, suite.mockFeeMarketKeeper, suite.txDecoder, &EVMMempoolConfig{
				TxPool:     suite.mempool.txPool,
				CosmosPool: suite.cosmosPool,
			})

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
			suite.mempool = NewEVMMempool(suite.ctxFunc, suite.mockVMKeeper, suite.mockFeeMarketKeeper, suite.txDecoder, &EVMMempoolConfig{
				TxPool:     suite.mempool.txPool,
				CosmosPool: suite.cosmosPool,
			})

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
			suite.mempool = NewEVMMempool(suite.ctxFunc, suite.mockVMKeeper, suite.mockFeeMarketKeeper, suite.txDecoder, &EVMMempoolConfig{
				TxPool:     suite.mempool.txPool,
				CosmosPool: suite.cosmosPool,
			})
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
			suite.mempool = NewEVMMempool(suite.ctxFunc, suite.mockVMKeeper, suite.mockFeeMarketKeeper, suite.txDecoder, &EVMMempoolConfig{
				TxPool:     suite.mempool.txPool,
				CosmosPool: suite.cosmosPool,
			})
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
				// Create Cosmos transaction with very high fee but wrong denomination
				// This should be deprioritized despite the high fee
				highFeeWrongDenomTx := suite.createCosmosTransaction("uatom", 50000000000) // 50 gwei but wrong denom

				// Create Cosmos transaction with lower fee but correct denomination
				// This should be prioritized over wrong denom despite lower fee
				lowFeeCorrectDenomTx := suite.createCosmosTransaction("wei", 3000000000) // 3 gwei correct denom

				// Create EVM transaction with medium fee (always uses correct denom internally)
				mediumFeeEVMTx, privKey, err := suite.createEVMTransaction(big.NewInt(5000000000)) // 5 gwei
				require.NoError(suite.T(), err)
				fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
				suite.addAccountToStateDB(fromAddr, big.NewInt(100000000000000000))

				// Insert in order: wrong denom (highest fee), correct denom (lowest fee), EVM (medium fee)
				err = suite.mempool.Insert(suite.ctx, highFeeWrongDenomTx)
				require.NoError(suite.T(), err)
				err = suite.mempool.Insert(suite.ctx, lowFeeCorrectDenomTx)
				require.NoError(suite.T(), err)
				err = suite.mempool.Insert(suite.ctx, mediumFeeEVMTx)
				require.NoError(suite.T(), err)
			},
			verifyFunc: func(t *testing.T, iterator cosmosMempool.Iterator) {
				// First transaction should be EVM (highest fee in correct denom)
				tx1 := iterator.Tx()
				require.NotNil(t, tx1)
				if ethMsg, ok := tx1.GetMsgs()[0].(*evmtypes.MsgEthereumTx); ok {
					ethTx := ethMsg.AsTransaction()
					require.Equal(t, big.NewInt(5000000000), ethTx.GasPrice())
				} else {
					t.Fatal("Expected first transaction to be EVM transaction")
				}

				// Move to next transaction
				iterator = iterator.Next()
				require.NotNil(t, iterator)

				// Second transaction should be the correct denom Cosmos tx (3 gwei wei)
				// This should come before the wrong denom tx despite much lower fee
				tx2 := iterator.Tx()
				require.NotNil(t, tx2)
				if bankMsg, ok := tx2.GetMsgs()[0].(*banktypes.MsgSend); ok {
					// Verify it's a bank message (Cosmos transaction)
					require.NotNil(t, bankMsg)
					// Verify it has the correct denomination fee
					if feeTx, ok := tx2.(sdk.FeeTx); ok {
						fees := feeTx.GetFee()
						require.Len(t, fees, 1)
						require.Equal(t, "wei", fees[0].Denom)
						require.Equal(t, int64(3000000000), fees[0].Amount.Int64())
					}
				} else {
					t.Fatal("Expected second transaction to be Cosmos transaction with correct denomination")
				}

				// The wrong denomination transaction (50 gwei uatom) should come last
				// due to denomination mismatch despite having the highest raw fee amount
			},
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Reset state for each test by creating new pools
			// Don't create cosmosPool here - let NewEVMMempool create it with proper priority logic

			// Create a fresh EVM pool
			suite.mockChain = mocks.NewMockBlockChain(suite.mockVMKeeper)
			legacyPool := legacypool.New(legacypool.DefaultConfig, suite.mockChain)
			reserver := &mocks.MockReserver{}
			err := legacyPool.Init(1000000000, suite.mockChain.CurrentBlock(), reserver)
			require.NoError(suite.T(), err)
			txPool := &txpool.TxPool{
				Subpools: []txpool.SubPool{legacyPool},
			}

			// Let the constructor create the cosmosPool with correct priority logic
			suite.mempool = NewEVMMempool(suite.ctxFunc, suite.mockVMKeeper, suite.mockFeeMarketKeeper, suite.txDecoder, &EVMMempoolConfig{
				TxPool:     txPool,
				CosmosPool: nil, // Let it create its own with correct priority logic
			})
			// Update the suite's cosmosPool to point to the one created by NewEVMMempool
			suite.cosmosPool = suite.mempool.cosmosPool
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
	mockFeeMarketKeeper := &mocks.MockFeeMarketKeeper{
		BlockGasWanted: 1000000, // 1M gas
	}
	cosmosPool := cosmosMempool.DefaultPriorityMempool()
	txDecoder := encodingConfig.TxConfig.TxDecoder()
	testChain := mocks.NewMockBlockChain(mockVMKeeper)
	legacyPool := legacypool.New(legacypool.DefaultConfig, testChain)
	txPool := &txpool.TxPool{
		Subpools: []txpool.SubPool{legacyPool},
	}
	db := dbm.NewMemDB()
	storeKey := storetypes.NewKVStoreKey("test")
	cms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	_ = cms.LoadLatestVersion()
	ctxFunc := func(height int64, prove bool) (sdk.Context, error) {
		return sdk.NewContext(cms, cmtproto.Header{}, false, log.NewNopLogger()), nil
	}
	mpool := NewEVMMempool(ctxFunc, mockVMKeeper, mockFeeMarketKeeper, txDecoder, &EVMMempoolConfig{
		TxPool:     txPool,
		CosmosPool: cosmosPool,
	})

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

func (suite *MempoolTestSuite) TestSelectBy() {
	tests := []struct {
		name          string
		setupTxs      func()
		filterFunc    func(sdk.Tx) bool
		expectedCalls int // Number of transactions the filter should be called with
		verifyFunc    func(t *testing.T)
	}{
		{
			name:     "empty mempool - no infinite loop",
			setupTxs: func() {},
			filterFunc: func(tx sdk.Tx) bool {
				return true // Accept all
			},
			expectedCalls: 1, // Called once even for empty pool to check for transactions
			verifyFunc: func(t *testing.T) {
				// Should not hang or crash
			},
		},
		{
			name: "single cosmos transaction - terminates properly",
			setupTxs: func() {
				cosmosTx := suite.createCosmosTransaction("wei", 2000)
				err := suite.cosmosPool.Insert(suite.ctx, cosmosTx)
				require.NoError(suite.T(), err)
			},
			filterFunc: func(tx sdk.Tx) bool {
				return false // Reject first transaction - should stop immediately
			},
			expectedCalls: 1,
			verifyFunc: func(t *testing.T) {
				require.Equal(t, 1, suite.cosmosPool.CountTx())
			},
		},
		{
			name: "reject first transaction - should stop immediately",
			setupTxs: func() {
				// Add multiple transactions
				for i := 0; i < 5; i++ {
					cosmosTx := suite.createCosmosTransaction("wei", int64(1000+i*100))
					err := suite.cosmosPool.Insert(suite.ctx, cosmosTx)
					require.NoError(suite.T(), err)
				}
			},
			filterFunc: func(tx sdk.Tx) bool {
				return false // Reject first - should stop after first call
			},
			expectedCalls: 1, // Should call filter only once
			verifyFunc: func(t *testing.T) {
				require.Equal(t, 5, suite.cosmosPool.CountTx())
			},
		},
		{
			name: "mixed EVM and cosmos - reject first transaction",
			setupTxs: func() {
				// Add EVM transactions
				for i := 0; i < 3; i++ {
					evmTx, privKey, err := suite.createEVMTransaction(big.NewInt(int64(1000000000 + i*1000000000)))
					require.NoError(suite.T(), err)
					fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
					suite.addAccountToStateDB(fromAddr, big.NewInt(100000000000000000))
					err = suite.mempool.Insert(suite.ctx, evmTx)
					require.NoError(suite.T(), err)
				}
				// Add Cosmos transactions
				for i := 0; i < 3; i++ {
					cosmosTx := suite.createCosmosTransaction("wei", int64(1000000000+i*1000000000))
					err := suite.cosmosPool.Insert(suite.ctx, cosmosTx)
					require.NoError(suite.T(), err)
				}
			},
			filterFunc: func(tx sdk.Tx) bool {
				return false // Reject first transaction
			},
			expectedCalls: 1, // Should stop after first rejection
			verifyFunc: func(t *testing.T) {
				// Both pools should still have their transactions
				require.Equal(t, 3, suite.cosmosPool.CountTx())
				evmPending, _ := suite.mempool.txPool.Stats()
				require.Equal(t, 3, evmPending)
			},
		},
		{
			name: "accept high fee transactions until low fee encountered",
			setupTxs: func() {
				// Add transactions with different fees (cosmos mempool will order by priority)
				// Higher fee transactions should come first
				for i := 5; i >= 1; i-- { // Create in reverse order so highest fee is processed first
					cosmosTx := suite.createCosmosTransaction("wei", int64(i*1000)) // 5000, 4000, 3000, 2000, 1000
					err := suite.cosmosPool.Insert(suite.ctx, cosmosTx)
					require.NoError(suite.T(), err)
				}
			},
			filterFunc: func(tx sdk.Tx) bool {
				// Accept transactions with fees >= 3000, reject lower
				if feeTx, ok := tx.(sdk.FeeTx); ok {
					fees := feeTx.GetFee()
					if len(fees) > 0 {
						return fees[0].Amount.Int64() >= 3000
					}
				}
				return false
			},
			expectedCalls: -1, // Don't check exact count due to priority ordering complexity
			verifyFunc: func(t *testing.T) {
				require.Equal(t, 5, suite.cosmosPool.CountTx())
			},
		},
		{
			name: "single EVM transaction - terminates properly",
			setupTxs: func() {
				evmTx, privKey, err := suite.createEVMTransaction(big.NewInt(2000000000))
				require.NoError(suite.T(), err)
				fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
				suite.addAccountToStateDB(fromAddr, big.NewInt(100000000000000000))
				err = suite.mempool.Insert(suite.ctx, evmTx)
				require.NoError(suite.T(), err)
			},
			filterFunc: func(tx sdk.Tx) bool {
				return false // Reject first transaction
			},
			expectedCalls: 1,
			verifyFunc: func(t *testing.T) {
				evmPending, _ := suite.mempool.txPool.Stats()
				require.Equal(t, 1, evmPending)
			},
		},
		{
			name: "mixed pools - reject first transaction",
			setupTxs: func() {
				// Add 1 EVM transaction
				evmTx, privKey, err := suite.createEVMTransaction(big.NewInt(5000000000))
				require.NoError(suite.T(), err)
				fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
				suite.addAccountToStateDB(fromAddr, big.NewInt(100000000000000000))
				err = suite.mempool.Insert(suite.ctx, evmTx)
				require.NoError(suite.T(), err)
				// Add multiple cosmos transactions
				for i := 0; i < 4; i++ {
					cosmosTx := suite.createCosmosTransaction("wei", int64(1000000000+i*500000000))
					err := suite.cosmosPool.Insert(suite.ctx, cosmosTx)
					require.NoError(suite.T(), err)
				}
			},
			filterFunc: func(tx sdk.Tx) bool {
				return false // Reject first transaction
			},
			expectedCalls: 1, // Should stop after first rejection
			verifyFunc: func(t *testing.T) {
				// Verify both pools still have their transactions
				evmPending, _ := suite.mempool.txPool.Stats()
				require.Equal(t, 1, evmPending)
				require.Equal(t, 4, suite.cosmosPool.CountTx())
			},
		},
		{
			name: "accept multiple transactions until condition fails",
			setupTxs: func() {
				// Add transactions with predictable fees
				for i := 10; i > 0; i-- { // Create in reverse to get predictable ordering
					cosmosTx := suite.createCosmosTransaction("wei", int64(i*1000))
					err := suite.cosmosPool.Insert(suite.ctx, cosmosTx)
					require.NoError(suite.T(), err)
				}
			},
			filterFunc: func() func(sdk.Tx) bool {
				callCount := 0
				return func(tx sdk.Tx) bool {
					callCount++
					// Accept first 3 transactions, then reject
					return callCount <= 3
				}
			}(),
			expectedCalls: 4, // Accept 3, then reject 1 = 4 calls total
			verifyFunc: func(t *testing.T) {
				require.Equal(t, 10, suite.cosmosPool.CountTx())
			},
		},
		{
			name: "accept all transactions - processes until pool exhausted",
			setupTxs: func() {
				// Add limited number of transactions
				for i := 0; i < 3; i++ {
					cosmosTx := suite.createCosmosTransaction("wei", int64(1000+i*500))
					err := suite.cosmosPool.Insert(suite.ctx, cosmosTx)
					require.NoError(suite.T(), err)
				}
			},
			filterFunc: func(tx sdk.Tx) bool {
				return true // Accept all - should process all 3 and then stop when pool exhausted
			},
			expectedCalls: -1, // Don't check exact count - behavior depends on when pool exhausts
			verifyFunc: func(t *testing.T) {
				// All transactions should still be in pool (SelectBy doesn't remove them)
				require.Equal(t, 3, suite.cosmosPool.CountTx())
			},
		},
	}

	for _, tc := range tests {
		suite.T().Run(tc.name, func(t *testing.T) {
			// Reset state for each test
			suite.cosmosPool = cosmosMempool.DefaultPriorityMempool()

			// Create fresh EVM pool
			suite.mockChain = mocks.NewMockBlockChain(suite.mockVMKeeper)
			legacyPool := legacypool.New(legacypool.DefaultConfig, suite.mockChain)
			reserver := &mocks.MockReserver{}
			err := legacyPool.Init(1000000000, suite.mockChain.CurrentBlock(), reserver)
			require.NoError(suite.T(), err)
			txPool := &txpool.TxPool{
				Subpools: []txpool.SubPool{legacyPool},
			}

			suite.mempool = NewEVMMempool(suite.ctxFunc, suite.mockVMKeeper, suite.mockFeeMarketKeeper, suite.txDecoder, &EVMMempoolConfig{
				TxPool:     txPool,
				CosmosPool: suite.cosmosPool,
			})

			tc.setupTxs()

			// Track filter function calls to ensure we don't have infinite loops
			callCount := 0
			wrappedFilter := func(tx sdk.Tx) bool {
				callCount++
				// Prevent infinite loops by failing test if too many calls
				if callCount > 1000 {
					t.Fatal("Possible infinite loop detected - filter called more than 1000 times")
				}
				return tc.filterFunc(tx)
			}

			// Test SelectBy directly
			suite.mempool.SelectBy(suite.ctx, nil, wrappedFilter)

			// Assert that SelectBy completed without hanging
			require.True(t, callCount > 0, "Filter should have been called at least once")
			if tc.expectedCalls > 0 {
				require.Equal(t, tc.expectedCalls, callCount, "Filter should have been called expected number of times")
			}
		})
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
	mockFeeMarketKeeper := &mocks.MockFeeMarketKeeper{
		BlockGasWanted: 1000000, // 1M gas
	}
	cosmosPool := cosmosMempool.DefaultPriorityMempool()
	txDecoder := encodingConfig.TxConfig.TxDecoder()
	testChain := mocks.NewMockBlockChain(mockVMKeeper)
	legacyPool := legacypool.New(legacypool.DefaultConfig, testChain)
	txPool := &txpool.TxPool{
		Subpools: []txpool.SubPool{legacyPool},
	}
	ctxFunc := func(height int64, prove bool) (sdk.Context, error) {
		return ctx, nil
	}
	mpool := NewEVMMempool(ctxFunc, mockVMKeeper, mockFeeMarketKeeper, txDecoder, &EVMMempoolConfig{
		TxPool:     txPool,
		CosmosPool: cosmosPool,
	})

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

// Benchmark SelectBy - critical for block building performance
func BenchmarkSelectBy(b *testing.B) {
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
	mockFeeMarketKeeper := &mocks.MockFeeMarketKeeper{
		BlockGasWanted: 1000000, // 1M gas
	}
	cosmosPool := cosmosMempool.DefaultPriorityMempool()
	txDecoder := encodingConfig.TxConfig.TxDecoder()
	testChain := mocks.NewMockBlockChain(mockVMKeeper)
	legacyPool := legacypool.New(legacypool.DefaultConfig, testChain)
	txPool := &txpool.TxPool{
		Subpools: []txpool.SubPool{legacyPool},
	}
	ctxFunc := func(height int64, prove bool) (sdk.Context, error) {
		return ctx, nil
	}
	mpool := NewEVMMempool(ctxFunc, mockVMKeeper, mockFeeMarketKeeper, txDecoder, &EVMMempoolConfig{
		TxPool:     txPool,
		CosmosPool: cosmosPool,
	})

	// Pre-populate with mixed transactions for realistic block building scenario
	for i := 0; i < 50; i++ {
		// Create cosmos transactions
		fromAddr := sdk.AccAddress("test_from_address__")
		toAddr := sdk.AccAddress("test_to_address____")
		amount := sdk.NewCoins(sdk.NewInt64Coin("wei", 1000))
		bankMsg := banktypes.NewMsgSend(fromAddr, toAddr, amount)

		txBuilder := encodingConfig.TxConfig.NewTxBuilder()
		_ = txBuilder.SetMsgs(bankMsg)
		txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("wei", int64(1000000000+i*1000000))))
		txBuilder.SetGasLimit(200000)
		tx := txBuilder.GetTx()

		_ = cosmosPool.Insert(ctx, tx)
	}

	// Add some EVM transactions as well
	for i := 0; i < 0; i++ { // Skip EVM transactions in benchmark
		privKey, _ := crypto.GenerateKey()
		to := common.HexToAddress("0x1234567890123456789012345678901234567890")
		ethTx := ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    uint64(i),
			To:       &to,
			Value:    big.NewInt(1000),
			Gas:      21000,
			GasPrice: big.NewInt(int64(1000000000 + i*1000000)),
			Data:     nil,
		})

		signer := ethtypes.HomesteadSigner{}
		signedTx, _ := ethtypes.SignTx(ethTx, signer, privKey)

		// Add account to mock state
		fromAddr := crypto.PubkeyToAddress(privKey.PublicKey)
		balanceU256, _ := uint256.FromBig(big.NewInt(100000000000000000))
		mockKeeper := mockVMKeeper
		mockKeeper.AddAccount(fromAddr, balanceU256, uint64(i))

		msgEthTx := &evmtypes.MsgEthereumTx{}
		_ = msgEthTx.FromEthereumTx(signedTx)

		txBuilder := encodingConfig.TxConfig.NewTxBuilder()
		_ = txBuilder.SetMsgs(msgEthTx)
		evmSdkTx := txBuilder.GetTx()

		_ = mpool.Insert(ctx, evmSdkTx)
	}

	b.Run("SelectByAcceptAll", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mpool.SelectBy(ctx, nil, func(tx sdk.Tx) bool {
				return true // Accept first transaction and stop
			})
		}
	})

	b.Run("SelectByRejectFirst10", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			count := 0
			mpool.SelectBy(ctx, nil, func(tx sdk.Tx) bool {
				count++
				if count <= 10 {
					return false // Reject first 10
				}
				return true // Accept 11th transaction
			})
		}
	})

	b.Run("SelectByFeeThreshold", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mpool.SelectBy(ctx, nil, func(tx sdk.Tx) bool {
				// Realistic block building: accept transactions above fee threshold\n			if tx == nil {\n				return false\n			}
				if feeTx, ok := tx.(sdk.FeeTx); ok {
					fees := feeTx.GetFee()
					if len(fees) > 0 && fees[0].Denom == "wei" {
						return fees[0].Amount.Int64() >= 1500000000 // 1.5 gwei threshold
					}
				}
				// For EVM transactions, check gas price
				msgs := tx.GetMsgs()
				if len(msgs) == 1 {
					if ethMsg, ok := msgs[0].(*evmtypes.MsgEthereumTx); ok {
						ethTx := ethMsg.AsTransaction()
						return ethTx.GasPrice().Int64() >= 1500000000 // 1.5 gwei threshold
					}
				}
				return false
			})
		}
	})

	b.Run("SelectByLimitedCount", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			count := 0
			maxTxs := 5 // Simulate block gas limit
			mpool.SelectBy(ctx, nil, func(tx sdk.Tx) bool {
				count++
				return count <= maxTxs // Accept only first 5 transactions
			})
		}
	})

	b.Run("SelectByExhaustAll", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mpool.SelectBy(ctx, nil, func(tx sdk.Tx) bool {
				return false // Reject all - tests full exhaustion performance
			})
		}
	})
}
