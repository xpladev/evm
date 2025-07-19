package common

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	"github.com/cosmos/evm/utils"
	precisebanktypes "github.com/cosmos/evm/x/precisebank/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

const ModuleAccAddrPreciseBank = "cosmos12yfe2jaupmtjruwxsec7hg7er60fhaa4qh25lc"

var bypassAddrs = []sdk.AccAddress{
	sdk.MustAccAddressFromBech32(ModuleAccAddrPreciseBank),
}

// ParseHexAddress parses the address from the event attributes and checks if it is a bypass address.
func ParseHexAddress(event sdk.Event, key string) (addr common.Address, bypass bool, err error) {
	attr, ok := event.GetAttribute(key)
	if !ok {
		return addr, bypass, fmt.Errorf("event %q missing attribute %q", event.Type, key)
	}

	accAddr, err := sdk.AccAddressFromBech32(attr.Value)
	if err != nil {
		return addr, bypass, fmt.Errorf("invalid address %q: %w", attr.Value, err)
	}
	addr = common.BytesToAddress(accAddr.Bytes())

	for _, bypassAddr := range bypassAddrs {
		if bypassAddr.Equals(accAddr) {
			bypass = true
			break
		}
	}

	return addr, bypass, nil
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

	// Otherwise, use regular denom and convert to 18 decimals
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
