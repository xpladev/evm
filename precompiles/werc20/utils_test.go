package werc20_test

import (
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/cosmos/evm/testutil/integration/os/factory"
	"github.com/cosmos/evm/testutil/integration/os/keyring"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// callType defines the different ways to call the precompile contract
// for comprehensive testing scenarios.
type callType int

const (
	directCall callType = iota
	contractCall
)

// CallsData encapsulates all necessary data for making calls to the WERC20 precompile
// and related contracts during integration testing.
type CallsData struct {
	// sender is used for transactions that don't require specific test account behavior
	sender keyring.Key

	// precompileReverter contract addresses and ABI for testing revert scenarios
	precompileReverterAddr common.Address
	precompileReverterABI  abi.ABI

	precompileAddr common.Address
	precompileABI  abi.ABI
}

// getTxAndCallArgs constructs transaction and call arguments based on the call type.
// It configures the appropriate target address and ABI for direct calls vs contract calls.
func (cd CallsData) getTxAndCallArgs(
	callType callType,
	methodName string,
	args ...any,
) (evmtypes.EvmTxArgs, factory.CallArgs) {
	txArgs := evmtypes.EvmTxArgs{}
	callArgs := factory.CallArgs{}

	switch callType {
	case directCall:
		txArgs.To = &cd.precompileAddr
		callArgs.ContractABI = cd.precompileABI
	case contractCall:
		txArgs.To = &cd.precompileReverterAddr
		callArgs.ContractABI = cd.precompileReverterABI
	}

	callArgs.MethodName = methodName
	callArgs.Args = args

	// Set gas tip cap to zero for zero gas price in tests
	txArgs.GasTipCap = new(big.Int).SetInt64(0)
	// Use a high gas limit to skip estimation and simplify debugging
	txArgs.GasLimit = 1_000_000_000_000

	return txArgs, callArgs
}
