package keeper

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/cosmos/evm/utils"
	"github.com/cosmos/evm/x/erc20/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/store/prefix"
	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateNewTokenMapping creates a new token mapping and stores it in the state.
func (k Keeper) CreateNewTokenMapping(ctx sdk.Context, denom string) (types.TokenMapping, error) {
	mapping, err := types.NewTokenMappingSTRv2(denom)
	if err != nil {
		return types.TokenMapping{}, err
	}
	if account := k.evmKeeper.GetAccount(ctx, mapping.GetERC20Contract()); account != nil && account.IsContract() {
		return types.TokenMapping{}, errorsmod.Wrapf(types.ErrTokenMappingAlreadyExists, "token already exists for token %s", mapping.Erc20Address)
	}
	err = k.SetToken(ctx, mapping)
	if err != nil {
		return types.TokenMapping{}, err
	}
	return mapping, nil
}

// SetToken stores a token mapping, denom map and erc20 map.
func (k *Keeper) SetToken(ctx sdk.Context, mapping types.TokenMapping) error {
	if k.IsDenomRegistered(ctx, mapping.Denom) {
		return errorsmod.Wrapf(types.ErrTokenMappingAlreadyExists, "token already exists for denom %s", mapping.Denom)
	}
	if k.IsERC20Registered(ctx, mapping.GetERC20Contract()) {
		return errorsmod.Wrapf(types.ErrTokenMappingAlreadyExists, "token already exists for token %s", mapping.Erc20Address)
	}
	k.SetTokenMapping(ctx, mapping)
	k.SetDenomMap(ctx, mapping.Denom, mapping.GetID())
	k.SetERC20Map(ctx, mapping.GetERC20Contract(), mapping.GetID())
	return nil
}

// GetTokenMappings gets all registered token tokenMappings.
func (k Keeper) GetTokenMappings(ctx sdk.Context) []types.TokenMapping {
	tokenMappings := []types.TokenMapping{}

	k.IterateTokenMappings(ctx, func(tokenMapping types.TokenMapping) (stop bool) {
		tokenMappings = append(tokenMappings, tokenMapping)
		return false
	})

	return tokenMappings
}

// IterateTokenMappings iterates over all the stored token mappings.
func (k Keeper) IterateTokenMappings(ctx sdk.Context, cb func(tokenMapping types.TokenMapping) (stop bool)) {
	store := ctx.KVStore(k.storeKey)
	iterator := storetypes.KVStorePrefixIterator(store, types.KeyPrefixTokenMapping)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var tokenMapping types.TokenMapping
		k.cdc.MustUnmarshal(iterator.Value(), &tokenMapping)

		if cb(tokenMapping) {
			break
		}
	}
}

// GetTokenMappingID returns the mapping id for the specified token. Hex address or Denom can be used as token argument.
// If the token is not registered empty bytes are returned.
func (k Keeper) GetTokenMappingID(ctx sdk.Context, token string) []byte {
	if common.IsHexAddress(token) {
		addr := common.HexToAddress(token)
		return k.GetERC20Map(ctx, addr)
	}
	return k.GetDenomMap(ctx, token)
}

// GetTokenMapping gets a registered token mapping from the identifier.
func (k Keeper) GetTokenMapping(ctx sdk.Context, id []byte) (types.TokenMapping, bool) {
	if id == nil {
		return types.TokenMapping{}, false
	}

	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMapping)
	var tokenMapping types.TokenMapping
	bz := store.Get(id)
	if len(bz) == 0 {
		return types.TokenMapping{}, false
	}

	k.cdc.MustUnmarshal(bz, &tokenMapping)
	return tokenMapping, true
}

// SetTokenMapping stores a token mapping.
func (k Keeper) SetTokenMapping(ctx sdk.Context, tokenMapping types.TokenMapping) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMapping)
	key := tokenMapping.GetID()
	bz := k.cdc.MustMarshal(&tokenMapping)
	store.Set(key, bz)
}

// DeleteTokenMapping removes a token mapping.
func (k Keeper) DeleteTokenMapping(ctx sdk.Context, tokenMapping types.TokenMapping) {
	id := tokenMapping.GetID()
	k.deleteTokenMapping(ctx, id)
	k.deleteERC20Map(ctx, tokenMapping.GetERC20Contract())
	k.deleteDenomMap(ctx, tokenMapping.Denom)
	k.deleteAllowances(ctx, tokenMapping.GetERC20Contract())
}

// deleteTokenMapping deletes the token mapping for the given id.
func (k Keeper) deleteTokenMapping(ctx sdk.Context, id []byte) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMapping)
	store.Delete(id)
}

// GetERC20Map returns the token mapping id for the given address.
func (k Keeper) GetERC20Map(ctx sdk.Context, erc20 common.Address) []byte {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMappingByERC20)
	return store.Get(erc20.Bytes())
}

// GetDenomMap returns the token mapping id for the given denomination.
func (k Keeper) GetDenomMap(ctx sdk.Context, denom string) []byte {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMappingByDenom)
	return store.Get([]byte(denom))
}

// SetERC20Map sets the token mapping id for the given address.
func (k Keeper) SetERC20Map(ctx sdk.Context, erc20 common.Address, id []byte) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMappingByERC20)
	store.Set(erc20.Bytes(), id)
}

// deleteERC20Map deletes the token mapping id for the given address.
func (k Keeper) deleteERC20Map(ctx sdk.Context, erc20 common.Address) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMappingByERC20)
	store.Delete(erc20.Bytes())
}

// SetDenomMap sets the token mapping id for the denomination.
func (k Keeper) SetDenomMap(ctx sdk.Context, denom string, id []byte) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMappingByDenom)
	store.Set([]byte(denom), id)
}

// deleteDenomMap deletes the token mapping id for the given denom.
func (k Keeper) deleteDenomMap(ctx sdk.Context, denom string) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMappingByDenom)
	store.Delete([]byte(denom))
}

// IsTokenMappingRegistered - check if registered token tokenMapping is registered.
func (k Keeper) IsTokenMappingRegistered(ctx sdk.Context, id []byte) bool {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMapping)
	return store.Has(id)
}

// IsERC20Registered check if registered ERC20 token is registered.
func (k Keeper) IsERC20Registered(ctx sdk.Context, erc20 common.Address) bool {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMappingByERC20)
	return store.Has(erc20.Bytes())
}

// IsDenomRegistered check if registered coin denom is registered.
func (k Keeper) IsDenomRegistered(ctx sdk.Context, denom string) bool {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), types.KeyPrefixTokenMappingByDenom)
	return store.Has([]byte(denom))
}

// GetCoinAddress returns the corresponding ERC-20 contract address for the
// given denom.
// If the denom is not registered and its an IBC voucher, it returns the address
// from the hash of the ICS20's Denom Path.
func (k Keeper) GetCoinAddress(ctx sdk.Context, denom string) (common.Address, error) {
	id := k.GetDenomMap(ctx, denom)
	if len(id) == 0 {
		// if the denom is not registered, check if it is an IBC voucher
		return utils.GetIBCDenomAddress(denom)
	}

	tokenMapping, found := k.GetTokenMapping(ctx, id)
	if !found {
		// safety check, should never happen
		return common.Address{}, errorsmod.Wrapf(
			types.ErrTokenMappingNotFound, "coin '%s' not registered", denom,
		)
	}

	return tokenMapping.GetERC20Contract(), nil
}

// GetTokenDenom returns the denom associated with the tokenAddress or an error
// if the TokenMapping does not exist.
func (k Keeper) GetTokenDenom(ctx sdk.Context, tokenAddress common.Address) (string, error) {
	tokenMappingID := k.GetERC20Map(ctx, tokenAddress)
	if len(tokenMappingID) == 0 {
		return "", errorsmod.Wrapf(
			types.ErrTokenMappingNotFound, "token '%s' not registered", tokenAddress,
		)
	}

	tokenMapping, found := k.GetTokenMapping(ctx, tokenMappingID)
	if !found {
		// safety check, should never happen
		return "", errorsmod.Wrapf(
			types.ErrTokenMappingNotFound, "token '%s' not registered", tokenAddress,
		)
	}

	return tokenMapping.Denom, nil
}
