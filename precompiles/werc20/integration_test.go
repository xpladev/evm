package werc20_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	//nolint:revive // dot imports are fine for Ginkgo
	. "github.com/onsi/ginkgo/v2"
	//nolint:revive // dot imports are fine for Ginkgo
	. "github.com/onsi/gomega"

	"github.com/cosmos/evm/precompiles/erc20"
	"github.com/cosmos/evm/precompiles/testutil"
	"github.com/cosmos/evm/precompiles/werc20"
	"github.com/cosmos/evm/precompiles/werc20/testdata"
	testconstants "github.com/cosmos/evm/testutil/constants"
	"github.com/cosmos/evm/testutil/integration/os/factory"
	"github.com/cosmos/evm/testutil/integration/os/grpc"
	"github.com/cosmos/evm/testutil/integration/os/keyring"
	"github.com/cosmos/evm/testutil/integration/os/network"
	utiltx "github.com/cosmos/evm/testutil/tx"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	precisebanktypes "github.com/cosmos/evm/x/precisebank/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// -------------------------------------------------------------------------------------------------
// Integration test suite
// -------------------------------------------------------------------------------------------------

type PrecompileIntegrationTestSuite struct {
	network     *network.UnitTestNetwork
	factory     factory.TxFactory
	grpcHandler grpc.Handler
	keyring     keyring.Keyring

	wrappedCoinDenom string

	// WEVMOS related fields
	precompile        *werc20.Precompile
	precompileAddrHex string
}

// BalanceSnapshot represents a snapshot of account balances for testing
type BalanceSnapshot struct {
	IntegerBalance    *big.Int
	FractionalBalance *big.Int
}

// getBalanceSnapshot gets complete balance information using grpcHandler
func (is *PrecompileIntegrationTestSuite) getBalanceSnapshot(addr sdk.AccAddress) (*BalanceSnapshot, error) {
	// Get integer balance (uatom)
	intRes, err := is.grpcHandler.GetBalanceFromBank(addr, evmtypes.GetEVMCoinDenom())
	if err != nil {
		return nil, fmt.Errorf("failed to get integer balance: %w", err)
	}

	// Get fractional balance using the new grpcHandler method
	fracRes, err := is.grpcHandler.FractionalBalance(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to get fractional balance: %w", err)
	}

	return &BalanceSnapshot{
		IntegerBalance:    intRes.Balance.Amount.BigInt(),
		FractionalBalance: fracRes.FractionalBalance.Amount.BigInt(),
	}, nil
}

// expectBalanceChange verifies expected balance changes after operations
func (is *PrecompileIntegrationTestSuite) expectBalanceChange(
	addr sdk.AccAddress,
	beforeSnapshot *BalanceSnapshot,
	expectedIntegerDelta *big.Int,
	expectedFractionalDelta *big.Int,
	description string,
) {
	afterSnapshot, err := is.getBalanceSnapshot(addr)
	Expect(err).ToNot(HaveOccurred(), "failed to get balance snapshot for %s", description)

	actualIntegerDelta := new(big.Int).Sub(afterSnapshot.IntegerBalance, beforeSnapshot.IntegerBalance)
	actualFractionalDelta := new(big.Int).Sub(afterSnapshot.FractionalBalance, beforeSnapshot.FractionalBalance)

	Expect(actualIntegerDelta.Cmp(expectedIntegerDelta)).To(Equal(0),
		"integer balance delta mismatch for %s: expected %s, got %s",
		description, expectedIntegerDelta.String(), actualIntegerDelta.String())

	Expect(actualFractionalDelta.Cmp(expectedFractionalDelta)).To(Equal(0),
		"fractional balance delta mismatch for %s: expected %s, got %s",
		description, expectedFractionalDelta.String(), actualFractionalDelta.String())
}

func TestPrecompileIntegrationTestSuite(t *testing.T) {
	// Run Ginkgo integration tests
	RegisterFailHandler(Fail)
	RunSpecs(t, "WEVMOS precompile test suite")
}

// -------------------------------------------------------------------------------------------------
// Integration tests
// -------------------------------------------------------------------------------------------------

var _ = DescribeTableSubtree("a user interact with the WEVMOS precompiled contract", func(chainId testconstants.ChainID) {
	var (
		is                                         *PrecompileIntegrationTestSuite
		passCheck, failCheck                       testutil.LogCheckArgs
		transferCheck, depositCheck, withdrawCheck testutil.LogCheckArgs

		callsData CallsData

		txSender, user keyring.Key

		revertContractAddr common.Address
	)

	// Setup deposit amount with both integer and fractional parts to test borrow/carry scenarios
	var conversionFactor *big.Int
	switch chainId {
	case testconstants.SixDecimalsChainID:
		conversionFactor = big.NewInt(1e12) // For 6-decimal chains
	case testconstants.TwelveDecimalsChainID:
		conversionFactor = big.NewInt(1e6) // For 12-decimal chains
	default:
		conversionFactor = big.NewInt(1) // For 18-decimal chains
	}

	// Create deposit with 1000 integer units + fractional part
	depositAmount := big.NewInt(1000)
	depositAmount = depositAmount.Mul(depositAmount, conversionFactor)                                       // 1000 integer units
	depositFractional := new(big.Int).Div(new(big.Int).Mul(conversionFactor, big.NewInt(3)), big.NewInt(10)) // Half conversion factor as fractional
	depositAmount = depositAmount.Add(depositAmount, depositFractional)

	withdrawAmount := depositAmount
	transferAmount := big.NewInt(10) // Start with 10 integer units

	BeforeEach(func() {
		is = new(PrecompileIntegrationTestSuite)
		keyring := keyring.New(2)

		txSender = keyring.GetKey(0)
		user = keyring.GetKey(1)

		// Set the base fee to zero to allow for zero cost tx. The final gas cost is
		// not part of the logic tested here so this makes testing more easy.
		customGenesis := network.CustomGenesisState{}
		feemarketGenesis := feemarkettypes.DefaultGenesisState()
		feemarketGenesis.Params.NoBaseFee = true
		customGenesis[feemarkettypes.ModuleName] = feemarketGenesis

		// Reset evm config here for the standard case
		configurator := evmtypes.NewEVMConfigurator()
		configurator.ResetTestConfig()
		Expect(configurator.
			WithEVMCoinInfo(testconstants.ExampleChainCoinInfo[chainId]).
			Configure()).To(BeNil(), "expected no error setting the evm configurator")

		integrationNetwork := network.NewUnitTestNetwork(
			network.WithChainID(chainId),
			network.WithPreFundedAccounts(keyring.GetAllAccAddrs()...),
			network.WithCustomGenesis(customGenesis),
		)
		grpcHandler := grpc.NewIntegrationHandler(integrationNetwork)
		txFactory := factory.New(integrationNetwork, grpcHandler)

		is.network = integrationNetwork
		is.factory = txFactory
		is.grpcHandler = grpcHandler
		is.keyring = keyring

		is.wrappedCoinDenom = evmtypes.GetEVMCoinDenom()
		is.precompileAddrHex = network.GetWEVMOSContractHex(testconstants.ChainID{
			ChainID:    is.network.GetChainID(),
			EVMChainID: is.network.GetEIP155ChainID().Uint64(),
		})

		ctx := integrationNetwork.GetContext()

		// Perform some check before adding the precompile to the suite.

		// Check that WEVMOS is part of the native precompiles.
		erc20Params := is.network.App.Erc20Keeper.GetParams(ctx)
		Expect(erc20Params.NativePrecompiles).To(
			ContainElement(is.precompileAddrHex),
			"expected wevmos to be in the native precompiles",
		)
		_, found := is.network.App.BankKeeper.GetDenomMetaData(ctx, evmtypes.GetEVMCoinDenom())
		Expect(found).To(BeTrue(), "expected native token metadata to be registered")

		// Check that WEVMOS is registered in the token pairs map.
		tokenPairID := is.network.App.Erc20Keeper.GetTokenPairID(ctx, is.wrappedCoinDenom)
		tokenPair, found := is.network.App.Erc20Keeper.GetTokenPair(ctx, tokenPairID)
		Expect(found).To(BeTrue(), "expected wevmos precompile to be registered in the tokens map")
		Expect(tokenPair.Erc20Address).To(Equal(is.precompileAddrHex))

		precompileAddr := common.HexToAddress(is.precompileAddrHex)
		tokenPair = erc20types.NewTokenPair(
			precompileAddr,
			evmtypes.GetEVMCoinDenom(),
			erc20types.OWNER_MODULE,
		)
		precompile, err := werc20.NewPrecompile(
			tokenPair,
			is.network.App.BankKeeper,
			is.network.App.Erc20Keeper,
			is.network.App.TransferKeeper,
		)
		Expect(err).ToNot(HaveOccurred(), "failed to instantiate the werc20 precompile")
		is.precompile = precompile

		// Setup of the contract calling into the precompile to tests revert
		// edge cases and proper handling of snapshots.
		revertCallerContract, err := testdata.LoadWEVMOS9TestCaller()
		Expect(err).ToNot(HaveOccurred(), "failed to load werc20 reverter caller contract")

		txArgs := evmtypes.EvmTxArgs{}
		txArgs.GasTipCap = new(big.Int).SetInt64(0)
		txArgs.GasLimit = 1_000_000_000_000
		revertContractAddr, err = is.factory.DeployContract(
			txSender.Priv,
			txArgs,
			factory.ContractDeploymentData{
				Contract: revertCallerContract,
				ConstructorArgs: []any{
					common.HexToAddress(is.precompileAddrHex),
				},
			},
		)
		Expect(err).ToNot(HaveOccurred(), "failed to deploy werc20 reverter contract")
		Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

		// Support struct used to simplify transactions creation.
		callsData = CallsData{
			sender: txSender,

			precompileAddr: precompileAddr,
			precompileABI:  precompile.ABI,

			precompileReverterAddr: revertContractAddr,
			precompileReverterABI:  revertCallerContract.ABI,
		}

		// Utility types used to check the different events emitted.
		failCheck = testutil.LogCheckArgs{ABIEvents: is.precompile.Events}
		passCheck = failCheck.WithExpPass(true)
		withdrawCheck = passCheck.WithExpEvents(werc20.EventTypeWithdrawal)
		depositCheck = passCheck.WithExpEvents(werc20.EventTypeDeposit)
		transferCheck = passCheck.WithExpEvents(erc20.EventTypeTransfer)
	})
	Context("calling a specific wrapped coin method", func() {
		Context("and funds are part of the transaction", func() {
			When("the method is deposit", func() {
				It("it should return funds to sender and emit the event", func() {
					// Get initial balance snapshots using grpcHandler for accurate state
					// userBeforeSnapshot, err := is.getBalanceSnapshot(user.AccAddr)
					// Expect(err).ToNot(HaveOccurred(), "failed to get initial user balance")

					precompileBeforeSnapshot, err := is.getBalanceSnapshot(callsData.precompileAddr.Bytes())
					Expect(err).ToNot(HaveOccurred(), "failed to get initial precompile balance")

					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, werc20.DepositMethod)
					txArgs.Amount = depositAmount

					_, _, err = is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")

					// For direct deposit calls, the funds should return to the original sender (user)
					// So user balance should remain the same (deposited amount is returned)
					// is.expectBalanceChange(user.AccAddr, userBeforeSnapshot, big.NewInt(0), big.NewInt(0), "user after direct deposit")

					// Precompile should have zero balance (it's just a passthrough)
					is.expectBalanceChange(callsData.precompileAddr.Bytes(), precompileBeforeSnapshot, big.NewInt(0), big.NewInt(0), "precompile after direct deposit")
				})
				It("it should consume at least the deposit requested gas", func() {
					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, werc20.DepositMethod)
					txArgs.Amount = depositAmount

					_, ethRes, err := is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					Expect(ethRes.GasUsed).To(BeNumerically(">=", werc20.DepositRequiredGas), "expected different gas used for deposit")
				})
			})
			//nolint:dupl
			When("no calldata is provided", func() {
				It("it should call the receive which behave like deposit", func() {
					// Get initial balance snapshots using grpcHandler for accurate state
					userBeforeSnapshot, err := is.getBalanceSnapshot(user.AccAddr)
					Expect(err).ToNot(HaveOccurred(), "failed to get initial user balance")

					precompileBeforeSnapshot, err := is.getBalanceSnapshot(callsData.precompileAddr.Bytes())
					Expect(err).ToNot(HaveOccurred(), "failed to get initial precompile balance")

					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, "")
					txArgs.Amount = depositAmount

					_, _, err = is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					// For receive calls (no calldata), should behave like deposit
					// User balance should remain the same (deposited amount is returned)
					is.expectBalanceChange(user.AccAddr, userBeforeSnapshot, big.NewInt(0), big.NewInt(0), "user after receive deposit")

					// Precompile should have zero balance (it's just a passthrough)
					is.expectBalanceChange(callsData.precompileAddr.Bytes(), precompileBeforeSnapshot, big.NewInt(0), big.NewInt(0), "precompile after receive deposit")
				})
				It("it should consume at least the deposit requested gas", func() {
					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, werc20.DepositMethod)
					txArgs.Amount = depositAmount

					_, ethRes, err := is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					Expect(ethRes.GasUsed).To(BeNumerically(">=", werc20.DepositRequiredGas), "expected different gas used for receive")
				})
			})
			When("the specified method is too short", func() {
				It("it should call the fallback which behave like deposit", func() {
					// Get initial balance snapshots using grpcHandler for accurate state
					userBeforeSnapshot, err := is.getBalanceSnapshot(user.AccAddr)
					Expect(err).ToNot(HaveOccurred(), "failed to get initial user balance")

					precompileBeforeSnapshot, err := is.getBalanceSnapshot(callsData.precompileAddr.Bytes())
					Expect(err).ToNot(HaveOccurred(), "failed to get initial precompile balance")

					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, "")
					txArgs.Amount = depositAmount
					// Short method is directly set in the input to skip ABI validation
					txArgs.Input = []byte{1, 2, 3}

					_, _, err = is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					// For short method fallback, should behave like deposit
					// User balance should remain the same (deposited amount is returned)
					is.expectBalanceChange(user.AccAddr, userBeforeSnapshot, big.NewInt(0), big.NewInt(0), "user after short method fallback deposit")

					// Precompile should have zero balance (it's just a passthrough)
					is.expectBalanceChange(callsData.precompileAddr.Bytes(), precompileBeforeSnapshot, big.NewInt(0), big.NewInt(0), "precompile after short method fallback deposit")
				})
				It("it should consume at least the deposit requested gas", func() {
					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, "")
					txArgs.Amount = depositAmount
					// Short method is directly set in the input to skip ABI validation
					txArgs.Input = []byte{1, 2, 3}

					_, ethRes, err := is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					Expect(ethRes.GasUsed).To(BeNumerically(">=", werc20.DepositRequiredGas), "expected different gas used for fallback")
				})
			})
			When("the specified method does not exist", func() {
				It("it should call the fallback which behave like deposit", func() {
					// Get initial balance snapshots using grpcHandler for accurate state
					userBeforeSnapshot, err := is.getBalanceSnapshot(user.AccAddr)
					Expect(err).ToNot(HaveOccurred(), "failed to get initial user balance")

					precompileBeforeSnapshot, err := is.getBalanceSnapshot(callsData.precompileAddr.Bytes())
					Expect(err).ToNot(HaveOccurred(), "failed to get initial precompile balance")

					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, "")
					txArgs.Amount = depositAmount
					// Wrong method is directly set in the input to skip ABI validation
					txArgs.Input = []byte("nonExistingMethod")

					_, _, err = is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					// For non-existing methods, should fallback to deposit behavior
					// User balance should remain the same (deposited amount is returned)
					is.expectBalanceChange(user.AccAddr, userBeforeSnapshot, big.NewInt(0), big.NewInt(0), "user after non-existing method fallback deposit")

					// Precompile should have zero balance (it's just a passthrough)
					is.expectBalanceChange(callsData.precompileAddr.Bytes(), precompileBeforeSnapshot, big.NewInt(0), big.NewInt(0), "precompile after non-existing method fallback deposit")
				})
				It("it should consume at least the deposit requested gas", func() {
					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, "")
					txArgs.Amount = depositAmount
					// Wrong method is directly set in the input to skip ABI validation
					txArgs.Input = []byte("nonExistingMethod")

					_, ethRes, err := is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					Expect(ethRes.GasUsed).To(BeNumerically(">=", werc20.DepositRequiredGas), "expected different gas used for fallback")
				})
			})
		})
		Context("and funds are NOT part of the transaction", func() {
			When("the method is withdraw", func() {
				It("it should fail if user doesn't have enough funds", func() {
					// Get initial balance snapshots for both user and precompile
					userBeforeSnapshot, err := is.getBalanceSnapshot(user.AccAddr)
					Expect(err).ToNot(HaveOccurred(), "failed to get initial user balance")

					precompileBeforeSnapshot, err := is.getBalanceSnapshot(callsData.precompileAddr.Bytes())
					Expect(err).ToNot(HaveOccurred(), "failed to get initial precompile balance")

					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					// Create a new user with insufficient funds
					newUserAcc, newUserPriv := utiltx.NewAccAddressAndKey()
					newUserBalance := sdk.Coins{sdk.Coin{
						Denom:  evmtypes.GetEVMCoinDenom(),
						Amount: math.NewIntFromBigInt(withdrawAmount).Quo(precisebanktypes.ConversionFactor()).SubRaw(1),
					}}

					// Use the test factory to fund the new user instead of direct BankKeeper
					// This ensures proper state synchronization
					mintToNewUser := func() {
						ctx := is.network.GetContext()
						err := is.network.App.BankKeeper.SendCoins(ctx, user.AccAddr, newUserAcc, newUserBalance)
						Expect(err).ToNot(HaveOccurred(), "expected no error sending tokens")
					}
					mintToNewUser()
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, werc20.WithdrawMethod, withdrawAmount)

					_, _, err = is.factory.CallContractAndCheckLogs(newUserPriv, txArgs, callArgs, withdrawCheck)
					Expect(err).To(HaveOccurred(), "expected an error because not enough funds")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					// Original user and precompile balances should be unchanged
					is.expectBalanceChange(user.AccAddr, userBeforeSnapshot, big.NewInt(0), big.NewInt(0), "user after failed withdraw attempt")
					is.expectBalanceChange(callsData.precompileAddr.Bytes(), precompileBeforeSnapshot, big.NewInt(0), big.NewInt(0), "precompile after failed withdraw attempt")
				})
				It("it should be a no-op and emit the event", func() {
					// Get initial balance snapshots using grpcHandler
					userBeforeSnapshot, err := is.getBalanceSnapshot(user.AccAddr)
					Expect(err).ToNot(HaveOccurred(), "failed to get initial user balance")

					precompileBeforeSnapshot, err := is.getBalanceSnapshot(callsData.precompileAddr.Bytes())
					Expect(err).ToNot(HaveOccurred(), "failed to get initial precompile balance")

					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, werc20.WithdrawMethod, withdrawAmount)

					_, _, err = is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, withdrawCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					// Withdraw should be a no-op for WERC20, so balances should remain unchanged
					is.expectBalanceChange(user.AccAddr, userBeforeSnapshot, big.NewInt(0), big.NewInt(0), "user after withdraw no-op")
					is.expectBalanceChange(callsData.precompileAddr.Bytes(), precompileBeforeSnapshot, big.NewInt(0), big.NewInt(0), "precompile after withdraw no-op")
				})
				It("it should consume at least the withdraw requested gas", func() {
					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, werc20.WithdrawMethod, withdrawAmount)

					_, ethRes, err := is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, withdrawCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					Expect(ethRes.GasUsed).To(BeNumerically(">=", werc20.WithdrawRequiredGas), "expected different gas used for withdraw")
				})
			})
			//nolint:dupl
			When("no calldata is provided", func() {
				It("it should call the fallback which behave like deposit", func() {
					// Get initial balance snapshots using grpcHandler for accurate state
					userBeforeSnapshot, err := is.getBalanceSnapshot(user.AccAddr)
					Expect(err).ToNot(HaveOccurred(), "failed to get initial user balance")

					precompileBeforeSnapshot, err := is.getBalanceSnapshot(callsData.precompileAddr.Bytes())
					Expect(err).ToNot(HaveOccurred(), "failed to get initial precompile balance")

					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, "")
					txArgs.Amount = depositAmount

					_, _, err = is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					// For direct fallback calls (no calldata), should behave like deposit
					// User balance should remain the same (deposited amount is returned)
					is.expectBalanceChange(user.AccAddr, userBeforeSnapshot, big.NewInt(0), big.NewInt(0), "user after fallback deposit")

					// Precompile should have zero balance (it's just a passthrough)
					is.expectBalanceChange(callsData.precompileAddr.Bytes(), precompileBeforeSnapshot, big.NewInt(0), big.NewInt(0), "precompile after fallback deposit")
				})
				It("it should consume at least the deposit requested gas", func() {
					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, werc20.DepositMethod)
					txArgs.Amount = depositAmount

					_, ethRes, err := is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					Expect(ethRes.GasUsed).To(BeNumerically(">=", werc20.DepositRequiredGas), "expected different gas used for receive")
				})
			})
			When("the specified method is too short", func() {
				It("it should call the fallback which behave like deposit", func() {
					// Get initial balance snapshots using grpcHandler for accurate state
					userBeforeSnapshot, err := is.getBalanceSnapshot(user.AccAddr)
					Expect(err).ToNot(HaveOccurred(), "failed to get initial user balance")

					precompileBeforeSnapshot, err := is.getBalanceSnapshot(callsData.precompileAddr.Bytes())
					Expect(err).ToNot(HaveOccurred(), "failed to get initial precompile balance")

					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, "")
					txArgs.Amount = depositAmount
					// Short method is directly set in the input to skip ABI validation
					txArgs.Input = []byte{1, 2, 3}

					_, _, err = is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					// For short method fallback, should behave like deposit
					// User balance should remain the same (deposited amount is returned)
					is.expectBalanceChange(user.AccAddr, userBeforeSnapshot, big.NewInt(0), big.NewInt(0), "user after short method fallback deposit")

					// Precompile should have zero balance (it's just a passthrough)
					is.expectBalanceChange(callsData.precompileAddr.Bytes(), precompileBeforeSnapshot, big.NewInt(0), big.NewInt(0), "precompile after short method fallback deposit")
				})
				It("it should consume at least the deposit requested gas", func() {
					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, "")
					txArgs.Amount = depositAmount
					// Short method is directly set in the input to skip ABI validation
					txArgs.Input = []byte{1, 2, 3}

					_, ethRes, err := is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					Expect(ethRes.GasUsed).To(BeNumerically(">=", werc20.DepositRequiredGas), "expected different gas used for fallback")
				})
			})
			When("the specified method does not exist", func() {
				It("it should call the fallback which behave like deposit", func() {
					// Get initial balance snapshots using grpcHandler for accurate state
					userBeforeSnapshot, err := is.getBalanceSnapshot(user.AccAddr)
					Expect(err).ToNot(HaveOccurred(), "failed to get initial user balance")

					precompileBeforeSnapshot, err := is.getBalanceSnapshot(callsData.precompileAddr.Bytes())
					Expect(err).ToNot(HaveOccurred(), "failed to get initial precompile balance")

					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, "")
					txArgs.Amount = depositAmount
					// Wrong method is directly set in the input to skip ABI validation
					txArgs.Input = []byte("nonExistingMethod")

					_, _, err = is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					// For non-existing methods, should fallback to deposit behavior
					// User balance should remain the same (deposited amount is returned)
					is.expectBalanceChange(user.AccAddr, userBeforeSnapshot, big.NewInt(0), big.NewInt(0), "user after fallback deposit")

					// Precompile should have zero balance (it's just a passthrough)
					is.expectBalanceChange(callsData.precompileAddr.Bytes(), precompileBeforeSnapshot, big.NewInt(0), big.NewInt(0), "precompile after fallback deposit")
				})
				It("it should consume at least the deposit requested gas", func() {
					txArgs, callArgs := callsData.getTxAndCallArgs(directCall, "")
					txArgs.Amount = depositAmount
					// Wrong method is directly set in the input to skip ABI validation
					txArgs.Input = []byte("nonExistingMethod")

					_, ethRes, err := is.factory.CallContractAndCheckLogs(user.Priv, txArgs, callArgs, depositCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
					Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

					Expect(ethRes.GasUsed).To(BeNumerically(">=", werc20.DepositRequiredGas), "expected different gas used for fallback")
				})
			})
		})
	})
	Context("calling a reverter contract", func() {
		When("to call the deposit", func() {
			It("it should return funds to the contract caller and emit the event", func() {
				// Get initial balance snapshots for all relevant parties
				senderBeforeSnapshot, err := is.getBalanceSnapshot(txSender.AccAddr)
				Expect(err).ToNot(HaveOccurred(), "failed to get sender initial balance")

				contractBeforeSnapshot, err := is.getBalanceSnapshot(revertContractAddr.Bytes())
				Expect(err).ToNot(HaveOccurred(), "failed to get contract initial balance")

				precompileBeforeSnapshot, err := is.getBalanceSnapshot(callsData.precompileAddr.Bytes())
				Expect(err).ToNot(HaveOccurred(), "failed to get precompile initial balance")

				txArgs, callArgs := callsData.getTxAndCallArgs(contractCall, "depositWithRevert", false, false)
				txArgs.Amount = depositAmount

				_, _, err = is.factory.CallContractAndCheckLogs(txSender.Priv, txArgs, callArgs, depositCheck)
				Expect(err).ToNot(HaveOccurred(), "unexpected error calling the precompile")
				Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

				// For contract calls:
				// 1. txSender pays the deposit amount (balance decreases)
				// 2. Contract receives the deposit amount back from precompile (balance increases)
				// 3. Precompile acts as passthrough (balance stays 0)

				// Calculate expected balance changes based on chain decimals
				var expectedSenderIntegerDelta, expectedContractIntegerDelta *big.Int
				var expectedSenderExtendedDelta, expectedContractExtendedDelta *big.Int

				switch {
				case conversionFactor.Cmp(big.NewInt(1e12)) == 0: // 6-decimal chain
					expectedSenderIntegerDelta = new(big.Int).Neg((new(big.Int).Add(new(big.Int).Quo(depositAmount, conversionFactor), big.NewInt(1))))
					expectedContractIntegerDelta = new(big.Int).Quo(depositAmount, conversionFactor)
					expectedSenderExtendedDelta = new(big.Int).Sub(conversionFactor, depositFractional)
					expectedContractExtendedDelta = depositFractional
				case conversionFactor.Cmp(big.NewInt(1e6)) == 0: // 12-decimal chain
					expectedSenderIntegerDelta = new(big.Int).Neg((new(big.Int).Add(new(big.Int).Quo(depositAmount, conversionFactor), big.NewInt(1))))
					expectedContractIntegerDelta = new(big.Int).Quo(depositAmount, conversionFactor)
					expectedSenderExtendedDelta = new(big.Int).Sub(conversionFactor, depositFractional)
					expectedContractExtendedDelta = depositFractional
				default: // 18-decimal chain (conversionFactor = 1)
					expectedSenderIntegerDelta = new(big.Int).Neg(depositAmount)
					expectedContractIntegerDelta = depositAmount
					expectedSenderExtendedDelta = big.NewInt(0)
					expectedContractExtendedDelta = big.NewInt(0)
				}

				// Sender loses the deposit amount
				is.expectBalanceChange(txSender.AccAddr, senderBeforeSnapshot,
					expectedSenderIntegerDelta, expectedSenderExtendedDelta, "sender after contract deposit")

				// Contract receives the deposit amount (since it's the msg.sender to the precompile)
				is.expectBalanceChange(revertContractAddr.Bytes(), contractBeforeSnapshot,
					expectedContractIntegerDelta, expectedContractExtendedDelta, "contract after receiving deposit")

				// Precompile remains at zero balance
				is.expectBalanceChange(callsData.precompileAddr.Bytes(), precompileBeforeSnapshot,
					big.NewInt(0), big.NewInt(0), "precompile after contract deposit")
			})
		})
		DescribeTable("to call the deposit", func(before, after bool) {
			// Get initial balance snapshot for sender
			senderBeforeSnapshot, err := is.getBalanceSnapshot(txSender.AccAddr)
			Expect(err).ToNot(HaveOccurred(), "failed to get sender initial balance")

			txArgs, callArgs := callsData.getTxAndCallArgs(contractCall, "depositWithRevert", before, after)
			txArgs.Amount = depositAmount

			_, _, err = is.factory.CallContractAndCheckLogs(txSender.Priv, txArgs, callArgs, depositCheck)
			Expect(err).To(HaveOccurred(), "execution should have reverted")
			Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock")

			// When transaction reverts, all balance changes should be rolled back
			// So sender balance should remain exactly the same
			is.expectBalanceChange(txSender.AccAddr, senderBeforeSnapshot,
				big.NewInt(0), big.NewInt(0), "sender after reverted deposit")
		},
			Entry("it should not move funds and dont emit the event reverting before changing state", true, false),
			Entry("it should not move funds and dont emit the event reverting after changing state", false, true),
		)
	})
	Context("calling an erc20 method", func() {
		When("transferring tokens", func() {
			It("it should transfer tokens to a receiver using `transfer`", func() {
				// First, sender needs to deposit to get WERC20 tokens
				// Use a larger deposit amount to ensure sufficient balance for transfer
				depositForTransfer := new(big.Int).Mul(transferAmount, big.NewInt(10)) // 10x transfer amount
				txArgs, callArgs := callsData.getTxAndCallArgs(directCall, werc20.DepositMethod)
				txArgs.Amount = depositForTransfer
				_, _, err := is.factory.CallContractAndCheckLogs(txSender.Priv, txArgs, callArgs, depositCheck)
				Expect(err).ToNot(HaveOccurred(), "failed to deposit before transfer")
				Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock after deposit")

				// Get balance snapshots after deposit, before transfer
				senderBeforeSnapshot, err := is.getBalanceSnapshot(txSender.AccAddr)
				Expect(err).ToNot(HaveOccurred(), "failed to get sender initial balance")

				receiverBeforeSnapshot, err := is.getBalanceSnapshot(user.AccAddr)
				Expect(err).ToNot(HaveOccurred(), "failed to get receiver initial balance")

				// Now perform the transfer
				txArgs, transferArgs := callsData.getTxAndCallArgs(directCall, erc20.TransferMethod, user.Addr, transferAmount)

				_, _, err = is.factory.CallContractAndCheckLogs(txSender.Priv, txArgs, transferArgs, transferCheck)
				Expect(err).ToNot(HaveOccurred(), "unexpected result calling contract")
				Expect(is.network.NextBlock()).ToNot(HaveOccurred(), "error on NextBlock after transfer")

				// Calculate expected balance changes
				transferAmountIntegerDelta := big.NewInt(10) // 10 integer units
				transferAmountExtendedDelta := big.NewInt(0)

				// Sender loses the transfer amount
				is.expectBalanceChange(txSender.AccAddr, senderBeforeSnapshot,
					new(big.Int).Neg(transferAmountIntegerDelta), new(big.Int).Neg(transferAmountExtendedDelta), "sender after transfer")

				// Receiver gains the transfer amount
				is.expectBalanceChange(user.AccAddr, receiverBeforeSnapshot,
					transferAmountIntegerDelta, transferAmountExtendedDelta, "receiver after transfer")
			})
			It("it should fail to transfer tokens to a receiver using `transferFrom`", func() {
				// Get initial balance snapshots for both parties
				senderBeforeSnapshot, err := is.getBalanceSnapshot(txSender.AccAddr)
				Expect(err).ToNot(HaveOccurred(), "failed to get sender initial balance")

				receiverBeforeSnapshot, err := is.getBalanceSnapshot(user.AccAddr)
				Expect(err).ToNot(HaveOccurred(), "failed to get receiver initial balance")

				txArgs, transferArgs := callsData.getTxAndCallArgs(directCall, erc20.TransferFromMethod, txSender.Addr, user.Addr, transferAmount)

				insufficientAllowanceCheck := failCheck.WithErrContains(erc20.ErrInsufficientAllowance.Error())
				_, _, err = is.factory.CallContractAndCheckLogs(txSender.Priv, txArgs, transferArgs, insufficientAllowanceCheck)
				Expect(err).ToNot(HaveOccurred(), "unexpected result calling contract")

				// When transfer fails, balances should remain unchanged
				is.expectBalanceChange(txSender.AccAddr, senderBeforeSnapshot,
					big.NewInt(0), big.NewInt(0), "sender after failed transferFrom")

				is.expectBalanceChange(user.AccAddr, receiverBeforeSnapshot,
					big.NewInt(0), big.NewInt(0), "receiver after failed transferFrom")
			})
		})
		When("querying information", func() {
			Context("to retrieve a balance", func() {
				It("should return the correct balance for an existing account", func() {
					// Query the balance
					txArgs, balancesArgs := callsData.getTxAndCallArgs(directCall, erc20.BalanceOfMethod, txSender.Addr)

					_, ethRes, err := is.factory.CallContractAndCheckLogs(txSender.Priv, txArgs, balancesArgs, passCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected result calling contract")

					// Get expected balance using grpcHandler for accurate state
					expBalanceRes, err := is.grpcHandler.GetBalanceFromBank(txSender.AccAddr, is.wrappedCoinDenom)
					Expect(err).ToNot(HaveOccurred(), "failed to get balance from grpcHandler")

					var balance *big.Int
					err = is.precompile.UnpackIntoInterface(&balance, erc20.BalanceOfMethod, ethRes.Ret)
					Expect(err).ToNot(HaveOccurred(), "failed to unpack result")
					Expect(balance).To(Equal(expBalanceRes.Balance.Amount.BigInt()), "expected different balance")
				})
				It("should return 0 for a new account", func() {
					// Query the balance
					txArgs, balancesArgs := callsData.getTxAndCallArgs(directCall, erc20.BalanceOfMethod, utiltx.GenerateAddress())

					_, ethRes, err := is.factory.CallContractAndCheckLogs(txSender.Priv, txArgs, balancesArgs, passCheck)
					Expect(err).ToNot(HaveOccurred(), "unexpected result calling contract")

					var balance *big.Int
					err = is.precompile.UnpackIntoInterface(&balance, erc20.BalanceOfMethod, ethRes.Ret)
					Expect(err).ToNot(HaveOccurred(), "failed to unpack result")
					Expect(balance.Int64()).To(Equal(int64(0)), "expected different balance")
				})
			})
			It("should return the correct name", func() {
				txArgs, nameArgs := callsData.getTxAndCallArgs(directCall, erc20.NameMethod)

				_, ethRes, err := is.factory.CallContractAndCheckLogs(txSender.Priv, txArgs, nameArgs, passCheck)
				Expect(err).ToNot(HaveOccurred(), "unexpected result calling contract")

				var name string
				err = is.precompile.UnpackIntoInterface(&name, erc20.NameMethod, ethRes.Ret)
				Expect(err).ToNot(HaveOccurred(), "failed to unpack result")
				Expect(name).To(ContainSubstring("Cosmos EVM"), "expected different name")
			})

			It("should return the correct symbol", func() {
				txArgs, symbolArgs := callsData.getTxAndCallArgs(directCall, erc20.SymbolMethod)

				_, ethRes, err := is.factory.CallContractAndCheckLogs(txSender.Priv, txArgs, symbolArgs, passCheck)
				Expect(err).ToNot(HaveOccurred(), "unexpected result calling contract")

				var symbol string
				err = is.precompile.UnpackIntoInterface(&symbol, erc20.SymbolMethod, ethRes.Ret)
				Expect(err).ToNot(HaveOccurred(), "failed to unpack result")
				Expect(symbol).To(ContainSubstring("ATOM"), "expected different symbol")
			})

			It("should return the decimals", func() {
				txArgs, decimalsArgs := callsData.getTxAndCallArgs(directCall, erc20.DecimalsMethod)

				_, ethRes, err := is.factory.CallContractAndCheckLogs(txSender.Priv, txArgs, decimalsArgs, passCheck)
				Expect(err).ToNot(HaveOccurred(), "unexpected result calling contract")

				var decimals uint8
				err = is.precompile.UnpackIntoInterface(&decimals, erc20.DecimalsMethod, ethRes.Ret)
				Expect(err).ToNot(HaveOccurred(), "failed to unpack result")

				coinInfo := testconstants.ExampleChainCoinInfo[testconstants.ChainID{
					ChainID:    is.network.GetChainID(),
					EVMChainID: is.network.GetEIP155ChainID().Uint64(),
				}]
				Expect(decimals).To(Equal(uint8(coinInfo.Decimals)), "expected different decimals")
			},
			)
		})
	})
},
	Entry("6 decimals chain", testconstants.SixDecimalsChainID),
	Entry("12 decimals chain", testconstants.TwelveDecimalsChainID),
	Entry("18 decimals chain", testconstants.ExampleChainID),
)
