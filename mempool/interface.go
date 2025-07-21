package mempool

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"math/big"
)

// VMKeeperI defines the interface for VM keeper operations needed by the mempool
type VMKeeperI interface {
	GetBaseFee(ctx sdk.Context) *big.Int
	GetParams(ctx sdk.Context) evmtypes.Params
}
