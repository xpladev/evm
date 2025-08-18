package erc20

import (
	"fmt"

	"github.com/cosmos/evm/testutil/config"
	testconstants "github.com/cosmos/evm/testutil/constants"
	utiltx "github.com/cosmos/evm/testutil/tx"
	"github.com/cosmos/evm/x/erc20/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
)

func (s *KeeperTestSuite) TestTokenMappings() {
	var (
		ctx    sdk.Context
		req    *types.QueryTokenMappingsRequest
		expRes *types.QueryTokenMappingsResponse
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"no mappings registered",
			func() {
				req = &types.QueryTokenMappingsRequest{}
				expRes = &types.QueryTokenMappingsResponse{
					Pagination: &query.PageResponse{
						Total: 1,
					},
					TokenMappings: testconstants.ExampleTokenMappings,
				}
			},
			true,
		},
		{
			"1 mapping registered w/pagination",
			func() {
				req = &types.QueryTokenMappingsRequest{
					Pagination: &query.PageRequest{Limit: 10, CountTotal: true},
				}
				mappings := testconstants.ExampleTokenMappings
				mapping := types.NewTokenMapping(utiltx.GenerateAddress(), "coin", types.OWNER_MODULE)
				s.network.App.GetErc20Keeper().SetTokenMapping(ctx, mapping)
				mappings = append(mappings, mapping)

				expRes = &types.QueryTokenMappingsResponse{
					Pagination:    &query.PageResponse{Total: uint64(len(mappings))},
					TokenMappings: mappings,
				}
			},
			true,
		},
		{
			"2 mappings registered wo/pagination",
			func() {
				req = &types.QueryTokenMappingsRequest{}
				mappings := testconstants.ExampleTokenMappings

				mapping := types.NewTokenMapping(utiltx.GenerateAddress(), "coin", types.OWNER_MODULE)
				mapping2 := types.NewTokenMapping(utiltx.GenerateAddress(), "coin2", types.OWNER_MODULE)
				s.network.App.GetErc20Keeper().SetTokenMapping(ctx, mapping)
				s.network.App.GetErc20Keeper().SetTokenMapping(ctx, mapping2)
				mappings = append(mappings, mapping, mapping2)

				expRes = &types.QueryTokenMappingsResponse{
					Pagination:    &query.PageResponse{Total: uint64(len(mappings))},
					TokenMappings: mappings,
				}
			},
			true,
		},
	}
	for _, tc := range testCases {
		s.Run(fmt.Sprintf("Case %s", tc.name), func() {
			s.SetupTest() // reset
			ctx = s.network.GetContext()

			tc.malleate()

			res, err := s.queryClient.TokenMappings(ctx, req)
			if tc.expPass {
				s.Require().NoError(err)
				s.Require().Equal(expRes.Pagination, res.Pagination)
				s.Require().ElementsMatch(expRes.TokenMappings, res.TokenMappings)
			} else {
				s.Require().Error(err)
			}
		})
	}
}

func (s *KeeperTestSuite) TestTokenMapping() {
	var (
		ctx    sdk.Context
		req    *types.QueryTokenMappingRequest
		expRes *types.QueryTokenMappingResponse
	)

	testCases := []struct {
		name     string
		malleate func()
		expPass  bool
	}{
		{
			"invalid token address",
			func() {
				req = &types.QueryTokenMappingRequest{}
				expRes = &types.QueryTokenMappingResponse{}
			},
			false,
		},
		{
			"token mapping not found",
			func() {
				req = &types.QueryTokenMappingRequest{
					Token: utiltx.GenerateAddress().Hex(),
				}
				expRes = &types.QueryTokenMappingResponse{}
			},
			false,
		},
		{
			"token mapping found",
			func() {
				addr := utiltx.GenerateAddress()
				mapping := types.NewTokenMapping(addr, "coin", types.OWNER_MODULE)
				err := s.network.App.GetErc20Keeper().SetToken(ctx, mapping)
				s.Require().NoError(err)
				req = &types.QueryTokenMappingRequest{
					Token: mapping.Erc20Address,
				}
				expRes = &types.QueryTokenMappingResponse{TokenMapping: mapping}
			},
			true,
		},
		{
			"token mapping not found - with erc20 existent",
			func() {
				addr := utiltx.GenerateAddress()
				mapping := types.NewTokenMapping(addr, "coin", types.OWNER_MODULE)
				s.network.App.GetErc20Keeper().SetERC20Map(ctx, addr, mapping.GetID())
				s.network.App.GetErc20Keeper().SetDenomMap(ctx, mapping.Denom, mapping.GetID())

				req = &types.QueryTokenMappingRequest{
					Token: mapping.Erc20Address,
				}
				expRes = &types.QueryTokenMappingResponse{TokenMapping: mapping}
			},
			false,
		},
	}
	for _, tc := range testCases {
		s.Run(fmt.Sprintf("Case %s", tc.name), func() {
			s.SetupTest() // reset
			ctx = s.network.GetContext()

			tc.malleate()

			res, err := s.queryClient.TokenMapping(ctx, req)
			if tc.expPass {
				s.Require().NoError(err)
				s.Require().Equal(expRes, res)
			} else {
				s.Require().Error(err)
			}
		})
	}
}

func (s *KeeperTestSuite) TestQueryParams() {
	s.SetupTest()
	ctx := s.network.GetContext()
	expParams := config.NewErc20GenesisState().Params

	res, err := s.queryClient.Params(ctx, &types.QueryParamsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(expParams, res.Params)
}
