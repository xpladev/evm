package types

import (
	"github.com/ethereum/go-ethereum/common"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// constants
const (
	// module name
	ModuleName = "erc20"

	// StoreKey to be used when creating the KVStore
	StoreKey = ModuleName

	// RouterKey to be used for message routing
	RouterKey = ModuleName
)

// ModuleAddress is the native module address for ERC-20
var ModuleAddress common.Address

func init() {
	ModuleAddress = common.BytesToAddress(authtypes.NewModuleAddress(ModuleName).Bytes())
}

// prefix bytes for the ERC-20 persistent store
const (
	prefixTokenMapping = iota + 1
	prefixTokenMappingByERC20
	prefixTokenMappingByDenom
	prefixSTRv2Addresses
	prefixAllowance
	prefixNativePrecompiles
	prefixDynamicPrecompiles
)

// KVStore key prefixes
var (
	KeyPrefixTokenMapping        = []byte{prefixTokenMapping}
	KeyPrefixTokenMappingByERC20 = []byte{prefixTokenMappingByERC20}
	KeyPrefixTokenMappingByDenom = []byte{prefixTokenMappingByDenom}
	KeyPrefixSTRv2Addresses      = []byte{prefixSTRv2Addresses}
	KeyPrefixAllowance           = []byte{prefixAllowance}
	KeyPrefixNativePrecompiles   = []byte{prefixNativePrecompiles}
	KeyPrefixDynamicPrecompiles  = []byte{prefixDynamicPrecompiles}
)

func AllowanceKey(
	erc20 common.Address,
	owner common.Address,
	spender common.Address,
) []byte {
	return append(append(erc20.Bytes(), owner.Bytes()...), spender.Bytes()...)
}
