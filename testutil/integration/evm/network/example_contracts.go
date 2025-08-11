package network

import (
	testconstants "github.com/cosmos/evm/testutil/constants"
)

// chainsWATOMHex is an utility map used to retrieve the WATOM contract
// address in hex format from the chain ID.
//
// TODO: refactor to define this in the example chain initialization and pass as function argument
var chainsWATOMHex = map[testconstants.ChainID]string{
	testconstants.ExampleChainID: testconstants.WATOMContractMainnet,
}

// GetWATOMContractHex returns the hex format of address for the WATOM contract
// given the chainID. If the chainID is not found, it defaults to the mainnet
// address.
func GetWATOMContractHex(chainID testconstants.ChainID) string {
	address, found := chainsWATOMHex[chainID]

	// default to mainnet address
	if !found {
		address = chainsWATOMHex[testconstants.ExampleChainID]
	}

	return address
}
