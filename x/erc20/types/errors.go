package types

import (
	errorsmod "cosmossdk.io/errors"
)

// errors
var (
	ErrERC20Disabled             = errorsmod.Register(ModuleName, 2, "erc20 module is disabled")
	ErrInternalTokenMapping      = errorsmod.Register(ModuleName, 3, "internal ethereum token mapping error")
	ErrTokenMappingNotFound      = errorsmod.Register(ModuleName, 4, "token mapping not found")
	ErrTokenMappingAlreadyExists = errorsmod.Register(ModuleName, 5, "token mapping already exists")
	ErrUndefinedOwner            = errorsmod.Register(ModuleName, 6, "undefined owner of contract mapping")
	ErrBalanceInvariance         = errorsmod.Register(ModuleName, 7, "post transfer balance invariant failed")
	ErrUnexpectedEvent           = errorsmod.Register(ModuleName, 8, "unexpected event")
	ErrABIPack                   = errorsmod.Register(ModuleName, 9, "contract ABI pack failed")
	ErrABIUnpack                 = errorsmod.Register(ModuleName, 10, "contract ABI unpack failed")
	ErrEVMDenom                  = errorsmod.Register(ModuleName, 11, "EVM denomination registration")
	ErrEVMCall                   = errorsmod.Register(ModuleName, 12, "EVM call unexpected error")
	ErrERC20TokenMappingDisabled = errorsmod.Register(ModuleName, 13, "erc20 token mapping is disabled")
	ErrInvalidIBC                = errorsmod.Register(ModuleName, 14, "invalid IBC transaction")
	ErrTokenMappingOwnedByModule = errorsmod.Register(ModuleName, 15, "token mapping owned by module")
	ErrNativeConversionDisabled  = errorsmod.Register(ModuleName, 16, "native coins manual conversion is disabled")
	ErrAllowanceNotFound         = errorsmod.Register(ModuleName, 17, "allowance not found")
	ErrInvalidAllowance          = errorsmod.Register(ModuleName, 18, "invalid allowance")
	ErrNegativeToken             = errorsmod.Register(ModuleName, 19, "token amount is negative")
	ErrExpectedEvent             = errorsmod.Register(ModuleName, 20, "expected event")
)
