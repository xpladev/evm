package erc20

import (
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/evm/testutil/integration/evm/network"
	utiltx "github.com/cosmos/evm/testutil/tx"
	"github.com/cosmos/evm/x/erc20"
	"github.com/cosmos/evm/x/erc20/types"
	transfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"

	"cosmossdk.io/math"
)

type GenesisTestSuite struct {
	suite.Suite
	network *network.UnitTestNetwork
	create  network.CreateEvmApp
	options []network.ConfigOption
	genesis types.GenesisState
}

const osmoERC20ContractAddr = "0x5D87876250185593977a6F94aF98877a5E7eD60E"

var osmoDenom = transfertypes.NewDenom("uosmo", transfertypes.NewHop(transfertypes.PortID, "channel-0"))

func NewGenesisTestSuite(create network.CreateEvmApp, options ...network.ConfigOption) *GenesisTestSuite {
	return &GenesisTestSuite{
		create:  create,
		options: options,
	}
}

func (s *GenesisTestSuite) SetupTest() {
	s.network = network.NewUnitTestNetwork(s.create, s.options...)
	s.genesis = *types.DefaultGenesisState()
}

func (s *GenesisTestSuite) TestERC20InitGenesis() {
	testCases := []struct {
		name         string
		genesisState types.GenesisState
	}{
		{
			name:         "empty genesis",
			genesisState: types.GenesisState{},
		},
		{
			name:         "default genesis",
			genesisState: *types.DefaultGenesisState(),
		},
		{
			name: "custom genesis",
			genesisState: types.NewGenesisState(
				types.DefaultParams(),
				[]types.TokenMapping{
					{
						Erc20Address:  osmoERC20ContractAddr,
						Denom:         osmoDenom.IBCDenom(),
						Enabled:       true,
						ContractOwner: types.OWNER_MODULE,
					},
				},
				[]types.Allowance{},
			),
		},
		{
			name: "custom genesis with allowances and enabled token mapping",
			genesisState: types.NewGenesisState(
				types.DefaultParams(),
				[]types.TokenMapping{
					{
						Erc20Address:  osmoERC20ContractAddr,
						Denom:         osmoDenom.IBCDenom(),
						Enabled:       true,
						ContractOwner: types.OWNER_MODULE,
					},
				},
				[]types.Allowance{
					{
						Erc20Address: osmoERC20ContractAddr,
						Owner:        utiltx.GenerateAddress().String(),
						Spender:      utiltx.GenerateAddress().String(),
						Value:        math.NewInt(100),
					},
				},
			),
		},
		{
			name: "custom genesis with allowances and disabled token mapping",
			genesisState: types.NewGenesisState(
				types.DefaultParams(),
				[]types.TokenMapping{
					{
						Erc20Address:  osmoERC20ContractAddr,
						Denom:         osmoDenom.IBCDenom(),
						Enabled:       false,
						ContractOwner: types.OWNER_MODULE,
					},
				},
				[]types.Allowance{
					{
						Erc20Address: osmoERC20ContractAddr,
						Owner:        utiltx.GenerateAddress().String(),
						Spender:      utiltx.GenerateAddress().String(),
						Value:        math.NewInt(100),
					},
				},
			),
		},
	}

	for _, tc := range testCases {
		gen := network.CustomGenesisState{
			types.ModuleName: &tc.genesisState, // #nosec G601
		}
		options := []network.ConfigOption{
			network.WithCustomGenesis(gen),
		}
		options = append(options, s.options...)
		nw := network.NewUnitTestNetwork(s.create, options...)

		params := nw.App.GetErc20Keeper().GetParams(nw.GetContext())

		tokenMappings := nw.App.GetErc20Keeper().GetTokenMappings(nw.GetContext())
		s.Require().Equal(tc.genesisState.Params, params)
		if len(tokenMappings) > 0 {
			s.Require().Equal(tc.genesisState.TokenMappings, tokenMappings, tc.name)
		} else {
			s.Require().Len(tc.genesisState.TokenMappings, 0, tc.name)
		}

		allowances := nw.App.GetErc20Keeper().GetAllowances(nw.GetContext())
		if len(allowances) > 0 {
			s.Require().Equal(tc.genesisState.Allowances, allowances, tc.name)
		} else {
			s.Require().Len(tc.genesisState.Allowances, 0, tc.name)
		}
	}
}

func (s *GenesisTestSuite) TestErc20ExportGenesis() {
	testGenCases := []struct {
		name         string
		genesisState types.GenesisState
	}{
		{
			name:         "empty genesis",
			genesisState: types.GenesisState{},
		},
		{
			name:         "default genesis",
			genesisState: *types.DefaultGenesisState(),
		},
		{
			name: "custom genesis with empty allowance",
			genesisState: types.NewGenesisState(
				types.DefaultParams(),
				[]types.TokenMapping{
					{
						Erc20Address:  osmoERC20ContractAddr,
						Denom:         osmoDenom.IBCDenom(),
						Enabled:       true,
						ContractOwner: types.OWNER_MODULE,
					},
				},
				[]types.Allowance{},
			),
		},
		{
			name: "custom genesis with allowances",
			genesisState: types.NewGenesisState(
				types.DefaultParams(),
				[]types.TokenMapping{
					{
						Erc20Address:  osmoERC20ContractAddr,
						Denom:         osmoDenom.IBCDenom(),
						Enabled:       true,
						ContractOwner: types.OWNER_MODULE,
					},
				},
				[]types.Allowance{
					{
						Erc20Address: osmoERC20ContractAddr,
						Owner:        utiltx.GenerateAddress().String(),
						Spender:      utiltx.GenerateAddress().String(),
						Value:        math.NewInt(100),
					},
					{
						Erc20Address: osmoERC20ContractAddr,
						Owner:        utiltx.GenerateAddress().String(),
						Spender:      utiltx.GenerateAddress().String(),
						Value:        math.NewInt(200),
					},
				},
			),
		},
	}

	for _, tc := range testGenCases {
		erc20Keeper := s.network.App.GetErc20Keeper()
		erc20.InitGenesis(s.network.GetContext(), *erc20Keeper, s.network.App.GetAccountKeeper(), tc.genesisState)
		s.Require().NotPanics(func() {
			genesisExported := erc20.ExportGenesis(s.network.GetContext(), *erc20Keeper)
			params := s.network.App.GetErc20Keeper().GetParams(s.network.GetContext())
			s.Require().Equal(genesisExported.Params, params)

			tokenMappings := s.network.App.GetErc20Keeper().GetTokenMappings(s.network.GetContext())
			if len(tokenMappings) > 0 {
				s.Require().Equal(genesisExported.TokenMappings, tokenMappings)
			} else {
				s.Require().Len(genesisExported.TokenMappings, 0)
			}
		})
	}
}
