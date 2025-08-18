package types

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/cometbft/cometbft/crypto/tmhash"

	cosmosevmtypes "github.com/cosmos/evm/types"
	"github.com/cosmos/evm/utils"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewTokenMappingSTRv2 creates a new TokenMapping instance in the context of the
// Single Token Representation v2.
//
// It derives the ERC-20 address from the hex suffix of the IBC denomination
// (e.g. ibc/DF63978F803A2E27CA5CC9B7631654CCF0BBC788B3B7F0A10200508E37C70992).
func NewTokenMappingSTRv2(denom string) (TokenMapping, error) {
	address, err := utils.GetIBCDenomAddress(denom)
	if err != nil {
		return TokenMapping{}, err
	}
	return TokenMapping{
		Erc20Address:  address.String(),
		Denom:         denom,
		Enabled:       true,
		ContractOwner: OWNER_MODULE,
	}, nil
}

// NewTokenMapping returns an instance of TokenMapping
func NewTokenMapping(erc20Address common.Address, denom string, contractOwner Owner) TokenMapping {
	return TokenMapping{
		Erc20Address:  erc20Address.String(),
		Denom:         denom,
		Enabled:       true,
		ContractOwner: contractOwner,
	}
}

// GetID returns the SHA256 hash of the ERC20 address and denomination
func (tp TokenMapping) GetID() []byte {
	id := tp.Erc20Address + "|" + tp.Denom
	return tmhash.Sum([]byte(id))
}

// GetErc20Contract casts the hex string address of the ERC20 to common.Address
func (tp TokenMapping) GetERC20Contract() common.Address {
	return common.HexToAddress(tp.Erc20Address)
}

// Validate performs a stateless validation of a TokenMapping
func (tp TokenMapping) Validate() error {
	if err := sdk.ValidateDenom(tp.Denom); err != nil {
		return err
	}

	return cosmosevmtypes.ValidateAddress(tp.Erc20Address)
}

// IsNativeCoin returns true if the owner of the ERC20 contract is the
// erc20 module account
func (tp TokenMapping) IsNativeCoin() bool {
	return tp.ContractOwner == OWNER_MODULE
}

// IsNativeERC20 returns true if the owner of the ERC20 contract is an EOA.
func (tp TokenMapping) IsNativeERC20() bool {
	return tp.ContractOwner == OWNER_EXTERNAL
}
