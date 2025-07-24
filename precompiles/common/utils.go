package common

import (
	"fmt"
	"math/big"

	"github.com/holiman/uint256"

	"github.com/cosmos/evm/utils"
	precisebanktypes "github.com/cosmos/evm/x/precisebank/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// ParseHexAddress parses the address from the event attributes and checks if it is a bypass address.
func ParseAddress(event sdk.Event, key string) (sdk.AccAddress, error) {
	attr, ok := event.GetAttribute(key)
	if !ok {
		return sdk.AccAddress{}, fmt.Errorf("event %q missing attribute %q", event.Type, key)
	}

	accAddr, err := sdk.AccAddressFromBech32(attr.Value)
	if err != nil {
		return sdk.AccAddress{}, fmt.Errorf("invalid address %q: %w", attr.Value, err)
	}

	return accAddr, nil
}

func ParseAmount(event sdk.Event) (*uint256.Int, error) {
	amountAttr, ok := event.GetAttribute(sdk.AttributeKeyAmount)
	if !ok {
		return nil, fmt.Errorf("event %q missing attribute %q", banktypes.EventTypeCoinSpent, sdk.AttributeKeyAmount)
	}

	amountCoins, err := sdk.ParseCoinsNormalized(amountAttr.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to parse coins from %q: %w", amountAttr.Value, err)
	}

	regularAmount := amountCoins.AmountOf(evmtypes.GetEVMCoinDenom())
	amount, err := utils.Uint256FromBigInt(evmtypes.ConvertAmountTo18DecimalsBigInt(regularAmount.BigInt()))
	if err != nil {
		return nil, fmt.Errorf("failed to convert coin amount to Uint256: %w", err)
	}
	return amount, nil
}

func ParseFractionalAmount(event sdk.Event) (*big.Int, error) {
	deltaAttr, ok := event.GetAttribute(precisebanktypes.AttributeKeyDelta)
	if !ok {
		return nil, fmt.Errorf("event %q missing attribute %q", precisebanktypes.EventTypeFractionalBalanceUpdated, sdk.AttributeKeyAmount)
	}

	delta, ok := sdkmath.NewIntFromString(deltaAttr.Value)
	if !ok {
		return nil, fmt.Errorf("failed to parse coins from %q", deltaAttr.Value)
	}

	return delta.BigInt(), nil
}
