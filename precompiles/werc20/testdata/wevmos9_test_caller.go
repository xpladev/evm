package testdata

import (
	contractutils "github.com/cosmos/evm/contracts/utils"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

func LoadWATOM9TestCaller() (evmtypes.CompiledContract, error) {
	return contractutils.LoadContractFromJSONFile("WATOM9TestCaller.json")
}
