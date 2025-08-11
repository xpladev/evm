package testdata

import (
	contractutils "github.com/cosmos/evm/contracts/utils"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// LoadWATOM9Contract load the WATOM9 contract from the json representation of
// the Solidity contract.
func LoadWATOM9Contract() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("WATOM9.json")
}
