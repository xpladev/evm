package types

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
)

// UnpackInterfaces implements UnpackInterfacesMessage.UnpackInterfaces
func (m QueryTraceTxRequest) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	for _, msg := range m.Predecessors {
		if err := msg.UnpackInterfaces(unpacker); err != nil {
			return err
		}
	}

	if m.Msg != nil {
		if err := m.Msg.UnpackInterfaces(unpacker); err != nil {
			return err
		}
	}

	return nil
}

func (m QueryTraceBlockRequest) UnpackInterfaces(unpacker codectypes.AnyUnpacker) error {
	for _, msg := range m.Txs {
		if err := msg.UnpackInterfaces(unpacker); err != nil {
			return err
		}
	}
	return nil
}

// Failed returns if the contract execution failed in vm errors
func (egr EstimateGasResponse) Failed() bool {
	return len(egr.VmError) > 0
}
