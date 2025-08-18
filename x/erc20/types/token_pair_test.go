package types_test

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/suite"

	"github.com/cometbft/cometbft/crypto/tmhash"

	utiltx "github.com/cosmos/evm/testutil/tx"
	"github.com/cosmos/evm/x/erc20/types"
)

type TokenMappingTestSuite struct {
	suite.Suite
}

func TestTokenMappingSuite(t *testing.T) {
	suite.Run(t, new(TokenMappingTestSuite))
}

func (suite *TokenMappingTestSuite) TestTokenMappingNew() {
	testCases := []struct {
		msg          string
		erc20Address common.Address
		denom        string
		owner        types.Owner
		expectPass   bool
	}{
		{msg: "Register token mapping - invalid starts with number", erc20Address: utiltx.GenerateAddress(), denom: "1test", owner: types.OWNER_MODULE, expectPass: false},
		{msg: "Register token mapping - invalid char '('", erc20Address: utiltx.GenerateAddress(), denom: "(test", owner: types.OWNER_MODULE, expectPass: false},
		{msg: "Register token mapping - invalid char '^'", erc20Address: utiltx.GenerateAddress(), denom: "^test", owner: types.OWNER_MODULE, expectPass: false},
		// TODO: (guille) should the "\" be allowed to support unicode names?
		{msg: "Register token mapping - invalid char '\\'", erc20Address: utiltx.GenerateAddress(), denom: "-test", owner: types.OWNER_MODULE, expectPass: false},
		// Invalid length
		{msg: "Register token mapping - invalid length token (0)", erc20Address: utiltx.GenerateAddress(), denom: "", owner: types.OWNER_MODULE, expectPass: false},
		{msg: "Register token mapping - invalid length token (1)", erc20Address: utiltx.GenerateAddress(), denom: "a", owner: types.OWNER_MODULE, expectPass: false},
		{msg: "Register token mapping - invalid length token (128)", erc20Address: utiltx.GenerateAddress(), denom: strings.Repeat("a", 129), owner: types.OWNER_MODULE, expectPass: false},
		{msg: "Register token mapping - pass", erc20Address: utiltx.GenerateAddress(), denom: "test", owner: types.OWNER_MODULE, expectPass: true},
	}

	for i, tc := range testCases {
		tp := types.NewTokenMapping(tc.erc20Address, tc.denom, tc.owner)
		err := tp.Validate()

		if tc.expectPass {
			suite.Require().NoError(err, "valid test %d failed: %s, %v", i, tc.msg)
		} else {
			suite.Require().Error(err, "invalid test %d passed: %s, %v", i, tc.msg)
		}
	}
}

func (suite *TokenMappingTestSuite) TestTokenMapping() {
	testCases := []struct {
		msg        string
		mapping    types.TokenMapping
		expectPass bool
	}{
		{msg: "Register token mapping - invalid address (no hex)", mapping: types.TokenMapping{"0x5dCA2483280D9727c80b5518faC4556617fb19ZZ", "test", true, types.OWNER_MODULE}, expectPass: false},
		{msg: "Register token mapping - invalid address (invalid length 1)", mapping: types.TokenMapping{"0x5dCA2483280D9727c80b5518faC4556617fb19", "test", true, types.OWNER_MODULE}, expectPass: false},
		{msg: "Register token mapping - invalid address (invalid length 2)", mapping: types.TokenMapping{"0x5dCA2483280D9727c80b5518faC4556617fb194FFF", "test", true, types.OWNER_MODULE}, expectPass: false},
		{msg: "pass", mapping: types.TokenMapping{utiltx.GenerateAddress().String(), "test", true, types.OWNER_MODULE}, expectPass: true},
	}

	for i, tc := range testCases {
		err := tc.mapping.Validate()

		if tc.expectPass {
			suite.Require().NoError(err, "valid test %d failed: %s, %v", i, tc.msg)
		} else {
			suite.Require().Error(err, "invalid test %d passed: %s, %v", i, tc.msg)
		}
	}
}

func (suite *TokenMappingTestSuite) TestGetID() {
	addr := utiltx.GenerateAddress()
	denom := "test"
	mapping := types.NewTokenMapping(addr, denom, types.OWNER_MODULE)
	id := mapping.GetID()
	expID := tmhash.Sum([]byte(addr.String() + "|" + denom))
	suite.Require().Equal(expID, id)
}

func (suite *TokenMappingTestSuite) TestGetERC20Contract() {
	expAddr := utiltx.GenerateAddress()
	denom := "test"
	mapping := types.NewTokenMapping(expAddr, denom, types.OWNER_MODULE)
	addr := mapping.GetERC20Contract()
	suite.Require().Equal(expAddr, addr)
}

func (suite *TokenMappingTestSuite) TestIsNativeCoin() {
	testCases := []struct {
		name       string
		mapping    types.TokenMapping
		expectPass bool
	}{
		{
			"no owner",
			types.TokenMapping{utiltx.GenerateAddress().String(), "test", true, types.OWNER_UNSPECIFIED},
			false,
		},
		{
			"external ERC20 owner",
			types.TokenMapping{utiltx.GenerateAddress().String(), "test", true, types.OWNER_EXTERNAL},
			false,
		},
		{
			"pass",
			types.TokenMapping{utiltx.GenerateAddress().String(), "test", true, types.OWNER_MODULE},
			true,
		},
	}

	for _, tc := range testCases {
		res := tc.mapping.IsNativeCoin()
		if tc.expectPass {
			suite.Require().True(res, tc.name)
		} else {
			suite.Require().False(res, tc.name)
		}
	}
}

func (suite *TokenMappingTestSuite) TestIsNativeERC20() {
	testCases := []struct {
		name       string
		mapping    types.TokenMapping
		expectPass bool
	}{
		{
			"no owner",
			types.TokenMapping{utiltx.GenerateAddress().String(), "test", true, types.OWNER_UNSPECIFIED},
			false,
		},
		{
			"module owner",
			types.TokenMapping{utiltx.GenerateAddress().String(), "test", true, types.OWNER_MODULE},
			false,
		},
		{
			"pass",
			types.TokenMapping{utiltx.GenerateAddress().String(), "test", true, types.OWNER_EXTERNAL},
			true,
		},
	}

	for _, tc := range testCases {
		res := tc.mapping.IsNativeERC20()
		if tc.expectPass {
			suite.Require().True(res, tc.name)
		} else {
			suite.Require().False(res, tc.name)
		}
	}
}

func (suite *TokenMappingTestSuite) TestNewTokenMappingSTRv2() {
	testCases := []struct {
		name            string
		denom           string
		expectPass      bool
		expectedError   string
		expectedMapping types.TokenMapping
	}{
		{
			name:          "fail to register token mapping - invalid denom (not ibc)",
			denom:         "testcoin",
			expectPass:    false,
			expectedError: "does not have 'ibc/' prefix",
		},
		{
			name:       "register token mapping - ibc denom",
			denom:      "ibc/DF63978F803A2E27CA5CC9B7631654CCF0BBC788B3B7F0A10200508E37C70992",
			expectPass: true,
			expectedMapping: types.TokenMapping{
				Denom:         "ibc/DF63978F803A2E27CA5CC9B7631654CCF0BBC788B3B7F0A10200508E37C70992",
				Erc20Address:  "0x631654CCF0BBC788b3b7F0a10200508e37c70992",
				Enabled:       true,
				ContractOwner: types.OWNER_MODULE,
			},
		},
	}

	for _, tc := range testCases {
		tokenMapping, err := types.NewTokenMappingSTRv2(tc.denom)
		if tc.expectPass {
			suite.Require().NoError(err)
			suite.Require().Equal(tokenMapping, tc.expectedMapping)
		} else {
			suite.Require().Error(err)
			suite.Require().ErrorContains(err, tc.expectedError)
		}

	}
}
