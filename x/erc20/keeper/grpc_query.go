package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	cosmosevmtypes "github.com/cosmos/evm/types"
	"github.com/cosmos/evm/x/erc20/types"

	"cosmossdk.io/store/prefix"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

var _ types.QueryServer = Keeper{}

// TokenMappings returns all registered mappings
func (k Keeper) TokenMappings(c context.Context, req *types.QueryTokenMappingsRequest) (*types.QueryTokenMappingsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	ctx := sdk.UnwrapSDKContext(c)

	var mappings []types.TokenMapping
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMapping)

	pageRes, err := query.Paginate(store, req.Pagination, func(_, value []byte) error {
		var mapping types.TokenMapping
		if err := k.cdc.Unmarshal(value, &mapping); err != nil {
			return err
		}
		mappings = append(mappings, mapping)
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryTokenMappingsResponse{
		TokenMappings: mappings,
		Pagination:    pageRes,
	}, nil
}

// TokenMapping returns a given registered token mapping
func (k Keeper) TokenMapping(c context.Context, req *types.QueryTokenMappingRequest) (*types.QueryTokenMappingResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	ctx := sdk.UnwrapSDKContext(c)

	// check if the token is a hex address, if not, check if it is a valid SDK
	// denom
	if err := cosmosevmtypes.ValidateAddress(req.Token); err != nil {
		if err := sdk.ValidateDenom(req.Token); err != nil {
			return nil, status.Errorf(
				codes.InvalidArgument,
				"invalid format for token %s, should be either hex ('0x...') cosmos denom", req.Token,
			)
		}
	}

	id := k.GetTokenMappingID(ctx, req.Token)

	if len(id) == 0 {
		return nil, status.Errorf(codes.NotFound, "token mapping with token '%s'", req.Token)
	}

	mapping, found := k.GetTokenMapping(ctx, id)
	if !found {
		return nil, status.Errorf(codes.NotFound, "token mapping with token '%s'", req.Token)
	}

	return &types.QueryTokenMappingResponse{TokenMapping: mapping}, nil
}

// Params returns the params of the erc20 module
func (k Keeper) Params(c context.Context, _ *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	params := k.GetParams(ctx)
	return &types.QueryParamsResponse{Params: params}, nil
}
