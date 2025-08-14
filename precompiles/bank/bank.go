//
// The bank package contains the implementation of the x/bank module precompile.
// The precompiles returns all bank's information in the original decimals
// representation stored in the module.

package bank

import (
	"embed"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"

	cmn "github.com/cosmos/evm/precompiles/common"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	storetypes "cosmossdk.io/store/types"
)

const (
	// GasBalances defines the gas cost for a single ERC-20 balanceOf query
	GasBalances = 2_851

	// GasTotalSupply defines the gas cost for a single ERC-20 totalSupply query
	GasTotalSupply = 2_477

	// GasSupplyOf defines the gas cost for a single ERC-20 supplyOf query, taken from totalSupply of ERC20
	GasSupplyOf = 2_477
)

var _ vm.PrecompiledContract = &Precompile{}

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

// Precompile defines the bank precompile
type Precompile struct {
	cmn.Precompile
	bankKeeper  cmn.BankKeeper
	erc20Keeper cmn.ERC20Keeper
}

// NewPrecompile creates a new bank Precompile instance implementing the
// PrecompiledContract interface.
func NewPrecompile(
	bankKeeper cmn.BankKeeper,
	erc20Keeper cmn.ERC20Keeper,
) (*Precompile, error) {
	newABI, err := cmn.LoadABI(f, "abi.json")
	if err != nil {
		return nil, err
	}

	// NOTE: we set an empty gas configuration to avoid extra gas costs
	// during the run execution
	p := &Precompile{
		Precompile: cmn.Precompile{
			ABI:                  newABI,
			KvGasConfig:          storetypes.GasConfig{},
			TransientKVGasConfig: storetypes.GasConfig{},
		},
		bankKeeper:  bankKeeper,
		erc20Keeper: erc20Keeper,
	}

	// SetAddress defines the address of the bank compile contract.
	p.SetAddress(common.HexToAddress(evmtypes.BankPrecompileAddress))

	return p, nil
}

// RequiredGas calculates the precompiled contract's base gas rate.
func (p Precompile) RequiredGas(input []byte) uint64 {
	// NOTE: This check avoid panicking when trying to decode the method ID
	if len(input) < 4 {
		return 0
	}

	methodID := input[:4]

	method, err := p.MethodById(methodID)
	if err != nil {
		// This should never happen since this method is going to fail during Run
		return 0
	}

	switch method.Name {
	case BalancesMethod:
		return GasBalances
	case TotalSupplyMethod:
		return GasTotalSupply
	case SupplyOfMethod:
		return GasSupplyOf
	}

	return 0
}

// Run executes the precompiled contract bank query methods defined in the ABI.
func (p Precompile) Run(evm *vm.EVM, contract *vm.Contract, readOnly bool) (bz []byte, err error) {
	ctx, _, method, initialGas, args, err := p.RunSetup(evm, contract, readOnly, p.IsTransaction)
	if err != nil {
		return nil, err
	}

	// This handles any out of gas errors that may occur during the execution of a precompile query.
	// It avoids panics and returns the out of gas error so the EVM can continue gracefully.
	defer cmn.HandleGasError(ctx, contract, initialGas, &err)()

	switch method.Name {
	// Bank queries
	case BalancesMethod:
		bz, err = p.Balances(ctx, contract, method, args)
	case TotalSupplyMethod:
		bz, err = p.TotalSupply(ctx, contract, method, args)
	case SupplyOfMethod:
		bz, err = p.SupplyOf(ctx, contract, method, args)
	default:
		return nil, fmt.Errorf(cmn.ErrUnknownMethod, method.Name)
	}

	if err != nil {
		return nil, err
	}

	cost := ctx.GasMeter().GasConsumed() - initialGas

	if !contract.UseGas(cost, nil, tracing.GasChangeCallPrecompiledContract) {
		return nil, vm.ErrOutOfGas
	}

	return bz, nil
}

// IsTransaction checks if the given method name corresponds to a transaction or query.
// It returns false since all bank methods are queries.
func (Precompile) IsTransaction(_ *abi.Method) bool {
	return false
}
