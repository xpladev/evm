package erc20

import (
	"fmt"

	utiltx "github.com/cosmos/evm/testutil/tx"
	"github.com/cosmos/evm/x/erc20/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (s *KeeperTestSuite) TestMintingEnabled() {
	var ctx sdk.Context
	sender := sdk.AccAddress(utiltx.GenerateAddress().Bytes())
	receiver := sdk.AccAddress(utiltx.GenerateAddress().Bytes())
	expMapping := types.NewTokenMapping(utiltx.GenerateAddress(), "coin", types.OWNER_MODULE)
	id := expMapping.GetID()

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"conversion is disabled globally",
			func() {
				params := types.DefaultParams()
				params.EnableErc20 = false
				s.network.App.GetErc20Keeper().SetParams(ctx, params) //nolint:errcheck
			},
			false,
		},
		{
			"token mapping not found",
			func() {},
			false,
		},
		{
			"conversion is disabled for the given mapping",
			func() {
				expMapping.Enabled = false
				s.network.App.GetErc20Keeper().SetTokenMapping(ctx, expMapping)
				s.network.App.GetErc20Keeper().SetDenomMap(ctx, expMapping.Denom, id)
				s.network.App.GetErc20Keeper().SetERC20Map(ctx, expMapping.GetERC20Contract(), id)
			},
			false,
		},
		{
			"token transfers are disabled",
			func() {
				expMapping.Enabled = true
				s.network.App.GetErc20Keeper().SetTokenMapping(ctx, expMapping)
				s.network.App.GetErc20Keeper().SetDenomMap(ctx, expMapping.Denom, id)
				s.network.App.GetErc20Keeper().SetERC20Map(ctx, expMapping.GetERC20Contract(), id)

				s.network.App.GetBankKeeper().SetSendEnabled(ctx, expMapping.Denom, false)
			},
			false,
		},
		{
			"token not registered",
			func() {
				s.network.App.GetErc20Keeper().SetDenomMap(ctx, expMapping.Denom, id)
				s.network.App.GetErc20Keeper().SetERC20Map(ctx, expMapping.GetERC20Contract(), id)
			},
			false,
		},
		{
			"receiver address is blocked (module account)",
			func() {
				s.network.App.GetErc20Keeper().SetTokenMapping(ctx, expMapping)
				s.network.App.GetErc20Keeper().SetDenomMap(ctx, expMapping.Denom, id)
				s.network.App.GetErc20Keeper().SetERC20Map(ctx, expMapping.GetERC20Contract(), id)

				acc := s.network.App.GetAccountKeeper().GetModuleAccount(ctx, types.ModuleName)
				receiver = acc.GetAddress()
			},
			false,
		},
		{
			"ok",
			func() {
				s.network.App.GetErc20Keeper().SetTokenMapping(ctx, expMapping)
				s.network.App.GetErc20Keeper().SetDenomMap(ctx, expMapping.Denom, id)
				s.network.App.GetErc20Keeper().SetERC20Map(ctx, expMapping.GetERC20Contract(), id)

				receiver = sdk.AccAddress(utiltx.GenerateAddress().Bytes())
			},
			true,
		},
	}

	for _, tc := range testCases {
		s.Run(fmt.Sprintf("Case %s", tc.name), func() {
			s.SetupTest() // reset
			ctx = s.network.GetContext()

			tc.malleate()

			mappping, err := s.network.App.GetErc20Keeper().MintingEnabled(ctx, sender, receiver, expMapping.Erc20Address)
			if tc.expPass {
				s.Require().NoError(err)
				s.Require().Equal(expMapping, mappping)
			} else {
				s.Require().Error(err)
			}
		})
	}
}
