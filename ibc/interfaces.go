package ibc

import (
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"

	ibctypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
)

type TransferKeeper interface {
	GetDenom(ctx sdk.Context, denomHash cmtbytes.HexBytes) (ibctypes.Denom, bool)
}
