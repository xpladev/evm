package erc20

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	testconstants "github.com/cosmos/evm/testutil/constants"
	utiltx "github.com/cosmos/evm/testutil/tx"
	"github.com/cosmos/evm/x/erc20/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (s *KeeperTestSuite) TestGetTokenMappings() {
	var (
		ctx    sdk.Context
		expRes []types.TokenMapping
	)

	testCases := []struct {
		name     string
		malleate func()
	}{
		{
			"no mapping registered", func() { expRes = testconstants.ExampleTokenMappings },
		},
		{
			"1 mapping registered",
			func() {
				mapping := types.NewTokenMapping(utiltx.GenerateAddress(), "coin", types.OWNER_MODULE)
				s.network.App.GetErc20Keeper().SetTokenMapping(ctx, mapping)
				expRes = testconstants.ExampleTokenMappings
				expRes = append(expRes, mapping)
			},
		},
		{
			"2 mappings registered",
			func() {
				mapping := types.NewTokenMapping(utiltx.GenerateAddress(), "coin", types.OWNER_MODULE)
				mapping2 := types.NewTokenMapping(utiltx.GenerateAddress(), "coin2", types.OWNER_MODULE)
				s.network.App.GetErc20Keeper().SetTokenMapping(ctx, mapping)
				s.network.App.GetErc20Keeper().SetTokenMapping(ctx, mapping2)
				expRes = testconstants.ExampleTokenMappings
				expRes = append(expRes, []types.TokenMapping{mapping, mapping2}...)
			},
		},
	}
	for _, tc := range testCases {
		s.Run(fmt.Sprintf("Case %s", tc.name), func() {
			s.SetupTest() // reset
			ctx = s.network.GetContext()

			tc.malleate()
			res := s.network.App.GetErc20Keeper().GetTokenMappings(ctx)

			s.Require().ElementsMatch(expRes, res, tc.name)
		})
	}
}

func (s *KeeperTestSuite) TestGetTokenMappingID() {
	baseDenom, err := sdk.GetBaseDenom()
	s.Require().NoError(err, "failed to get base denom")

	mapping := types.NewTokenMapping(utiltx.GenerateAddress(), baseDenom, types.OWNER_MODULE)

	testCases := []struct {
		name  string
		token string
		expID []byte
	}{
		{"nil token", "", nil},
		{"valid hex token", utiltx.GenerateAddress().Hex(), []byte{}},
		{"valid hex token", utiltx.GenerateAddress().String(), []byte{}},
	}
	for _, tc := range testCases {
		s.SetupTest()
		ctx := s.network.GetContext()

		s.network.App.GetErc20Keeper().SetTokenMapping(ctx, mapping)

		id := s.network.App.GetErc20Keeper().GetTokenMappingID(ctx, tc.token)
		if id != nil {
			s.Require().Equal(tc.expID, id, tc.name)
		} else {
			s.Require().Nil(id)
		}
	}
}

func (s *KeeperTestSuite) TestGetTokenMapping() {
	baseDenom, err := sdk.GetBaseDenom()
	s.Require().NoError(err, "failed to get base denom")

	mapping := types.NewTokenMapping(utiltx.GenerateAddress(), baseDenom, types.OWNER_MODULE)

	testCases := []struct {
		name string
		id   []byte
		ok   bool
	}{
		{"nil id", nil, false},
		{"valid id", mapping.GetID(), true},
		{"mapping not found", []byte{}, false},
	}
	for _, tc := range testCases {
		s.SetupTest()
		ctx := s.network.GetContext()

		s.network.App.GetErc20Keeper().SetTokenMapping(ctx, mapping)
		p, found := s.network.App.GetErc20Keeper().GetTokenMapping(ctx, tc.id)
		if tc.ok {
			s.Require().True(found, tc.name)
			s.Require().Equal(mapping, p, tc.name)
		} else {
			s.Require().False(found, tc.name)
		}
	}
}

func (s *KeeperTestSuite) TestDeleteTokenMapping() {
	tokenDenom := "random"

	var ctx sdk.Context
	mapping := types.NewTokenMapping(utiltx.GenerateAddress(), tokenDenom, types.OWNER_MODULE)
	id := mapping.GetID()

	testCases := []struct {
		name     string
		id       []byte
		malleate func()
		ok       bool
	}{
		{"nil id", nil, func() {}, false},
		{"mapping not found", []byte{}, func() {}, false},
		{"valid id", id, func() {}, true},
		{
			"delete token mapping",
			id,
			func() {
				s.network.App.GetErc20Keeper().DeleteTokenMapping(ctx, mapping)
			},
			false,
		},
	}
	for _, tc := range testCases {
		s.SetupTest()
		ctx = s.network.GetContext()
		err := s.network.App.GetErc20Keeper().SetToken(ctx, mapping)
		s.Require().NoError(err)

		tc.malleate()
		p, found := s.network.App.GetErc20Keeper().GetTokenMapping(ctx, tc.id)
		if tc.ok {
			s.Require().True(found, tc.name)
			s.Require().Equal(mapping, p, tc.name)
		} else {
			s.Require().False(found, tc.name)
		}
	}
}

func (s *KeeperTestSuite) TestIsTokenMappingRegistered() {
	baseDenom, err := sdk.GetBaseDenom()
	s.Require().NoError(err, "failed to get base denom")

	var ctx sdk.Context
	mapping := types.NewTokenMapping(utiltx.GenerateAddress(), baseDenom, types.OWNER_MODULE)

	testCases := []struct {
		name string
		id   []byte
		ok   bool
	}{
		{"valid id", mapping.GetID(), true},
		{"mapping not found", []byte{}, false},
	}
	for _, tc := range testCases {
		s.SetupTest()
		ctx = s.network.GetContext()

		s.network.App.GetErc20Keeper().SetTokenMapping(ctx, mapping)
		found := s.network.App.GetErc20Keeper().IsTokenMappingRegistered(ctx, tc.id)
		if tc.ok {
			s.Require().True(found, tc.name)
		} else {
			s.Require().False(found, tc.name)
		}
	}
}

func (s *KeeperTestSuite) TestIsERC20Registered() {
	var ctx sdk.Context
	addr := utiltx.GenerateAddress()
	mapping := types.NewTokenMapping(addr, "coin", types.OWNER_MODULE)

	testCases := []struct {
		name     string
		erc20    common.Address
		malleate func()
		ok       bool
	}{
		{"nil erc20 address", common.Address{}, func() {}, false},
		{"valid erc20 address", mapping.GetERC20Contract(), func() {}, true},
		{
			"deleted erc20 map",
			mapping.GetERC20Contract(),
			func() {
				s.network.App.GetErc20Keeper().DeleteTokenMapping(ctx, mapping)
			},
			false,
		},
	}
	for _, tc := range testCases {
		s.SetupTest()
		ctx = s.network.GetContext()

		err := s.network.App.GetErc20Keeper().SetToken(ctx, mapping)
		s.Require().NoError(err)

		tc.malleate()

		found := s.network.App.GetErc20Keeper().IsERC20Registered(ctx, tc.erc20)

		if tc.ok {
			s.Require().True(found, tc.name)
		} else {
			s.Require().False(found, tc.name)
		}
	}
}

func (s *KeeperTestSuite) TestIsDenomRegistered() {
	var ctx sdk.Context
	addr := utiltx.GenerateAddress()
	mapping := types.NewTokenMapping(addr, "coin", types.OWNER_MODULE)

	testCases := []struct {
		name     string
		denom    string
		malleate func()
		ok       bool
	}{
		{"empty denom", "", func() {}, false},
		{"valid denom", mapping.GetDenom(), func() {}, true},
		{
			"deleted denom map",
			mapping.GetDenom(),
			func() {
				s.network.App.GetErc20Keeper().DeleteTokenMapping(ctx, mapping)
			},
			false,
		},
	}
	for _, tc := range testCases {
		s.SetupTest()
		ctx = s.network.GetContext()

		err := s.network.App.GetErc20Keeper().SetToken(ctx, mapping)
		s.Require().NoError(err)

		tc.malleate()

		found := s.network.App.GetErc20Keeper().IsDenomRegistered(ctx, tc.denom)

		if tc.ok {
			s.Require().True(found, tc.name)
		} else {
			s.Require().False(found, tc.name)
		}
	}
}

func (s *KeeperTestSuite) TestGetTokenDenom() {
	var ctx sdk.Context
	tokenAddress := utiltx.GenerateAddress()
	tokenDenom := "token"

	testCases := []struct {
		name        string
		tokenDenom  string
		malleate    func()
		expError    bool
		errContains string
	}{
		{
			"denom found",
			tokenDenom,
			func() {
				mapping := types.NewTokenMapping(tokenAddress, tokenDenom, types.OWNER_MODULE)
				s.network.App.GetErc20Keeper().SetTokenMapping(ctx, mapping)
				s.network.App.GetErc20Keeper().SetERC20Map(ctx, tokenAddress, mapping.GetID())
			},
			true,
			"",
		},
		{
			"denom not found",
			tokenDenom,
			func() {
				address := utiltx.GenerateAddress()
				mapping := types.NewTokenMapping(address, tokenDenom, types.OWNER_MODULE)
				s.network.App.GetErc20Keeper().SetTokenMapping(ctx, mapping)
				s.network.App.GetErc20Keeper().SetERC20Map(ctx, address, mapping.GetID())
			},
			false,
			fmt.Sprintf("token '%s' not registered", tokenAddress),
		},
	}
	for _, tc := range testCases {
		s.Run(fmt.Sprintf("Case %s", tc.name), func() {
			s.SetupTest()
			ctx = s.network.GetContext()

			tc.malleate()
			res, err := s.network.App.GetErc20Keeper().GetTokenDenom(ctx, tokenAddress)

			if tc.expError {
				s.Require().NoError(err)
				s.Require().Equal(res, tokenDenom)
			} else {
				s.Require().Error(err, "expected an error while getting the token denom")
				s.Require().ErrorContains(err, tc.errContains)
			}
		})
	}
}

func (s *KeeperTestSuite) TestSetToken() {
	testCases := []struct {
		name     string
		mapping1 types.TokenMapping
		mapping2 types.TokenMapping
		expError bool
	}{
		{"same denom", types.NewTokenMapping(common.HexToAddress("0x1"), "denom1", types.OWNER_MODULE), types.NewTokenMapping(common.HexToAddress("0x2"), "denom1", types.OWNER_MODULE), true},
		{"same erc20", types.NewTokenMapping(common.HexToAddress("0x1"), "denom1", types.OWNER_MODULE), types.NewTokenMapping(common.HexToAddress("0x1"), "denom2", types.OWNER_MODULE), true},
		{"same mapping", types.NewTokenMapping(common.HexToAddress("0x1"), "denom1", types.OWNER_MODULE), types.NewTokenMapping(common.HexToAddress("0x1"), "denom1", types.OWNER_MODULE), true},
		{"two different mappings", types.NewTokenMapping(common.HexToAddress("0x1"), "denom1", types.OWNER_MODULE), types.NewTokenMapping(common.HexToAddress("0x2"), "denom2", types.OWNER_MODULE), false},
	}
	for _, tc := range testCases {
		s.SetupTest()
		ctx := s.network.GetContext()

		err := s.network.App.GetErc20Keeper().SetToken(ctx, tc.mapping1)
		s.Require().NoError(err)
		err = s.network.App.GetErc20Keeper().SetToken(ctx, tc.mapping2)
		if tc.expError {
			s.Require().Error(err)
		} else {
			s.Require().NoError(err)
		}
	}
}
