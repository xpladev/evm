package backend

import (
	"fmt"
	"math"

	"github.com/cosmos/gogoproto/proto"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	etherminttypes "github.com/xpladev/xpla/legacy/ethermint/x/evm/types"
)

// getEthereumTxMsg returns the message directly if it's an evmtypes.MsgEthereumTx,
// or converts it from an etherminttypes.MsgEthereumTx to an evmtypes.MsgEthereumTx before returning.
func getEthereumTxMsg(
	interfaceRegistry codectypes.InterfaceRegistry,
	tx sdk.Tx,
	msgIndex uint32,
) (*evmtypes.MsgEthereumTx, error) {
	if msgIndex > math.MaxInt32 {
        return nil, fmt.Errorf("message index %d exceeds maximum supported index %d", msgIndex, math.MaxInt32)
    }
    index := int(msgIndex)
	if index >= len(tx.GetMsgs()) {
		return nil, fmt.Errorf("message index %d out of bounds", msgIndex)
	}

	rawMsg := tx.GetMsgs()[index]

	return getEthereumMsg(interfaceRegistry, rawMsg)
}

func getEthereumMsg(interfaceRegistry codectypes.InterfaceRegistry, rawMsg sdk.Msg) (*evmtypes.MsgEthereumTx, error) {
	switch m := rawMsg.(type) {
	case *evmtypes.MsgEthereumTx:
		return m, nil

	case *etherminttypes.MsgEthereumTx:
		var legacyTxData etherminttypes.TxData
		if err := interfaceRegistry.UnpackAny(m.Data, &legacyTxData); err != nil {
			return nil, fmt.Errorf("failed to unpack legacy ethermint TxData: %w", err)
		}

		var txData proto.Message
		switch t := legacyTxData.(type) {
		case *etherminttypes.LegacyTx:
			txData = &evmtypes.LegacyTx{
				Nonce:    t.Nonce,
				GasPrice: t.GasPrice,
				GasLimit: t.GasLimit,
				To:       t.To,
				Amount:   t.Amount,
				Data:     t.Data,
				V:        t.V,
				R:        t.R,
				S:        t.S,
			}

		case *etherminttypes.AccessListTx:
			txData = &evmtypes.AccessListTx{
				ChainID:  t.ChainID,
				Nonce:    t.Nonce,
				GasPrice: t.GasPrice,
				GasLimit: t.GasLimit,
				To:       t.To,
				Amount:   t.Amount,
				Data:     t.Data,
				Accesses: accesses(t.Accesses),
				V:        t.V,
				R:        t.R,
				S:        t.S,
			}

		case *etherminttypes.DynamicFeeTx:
			txData = &evmtypes.DynamicFeeTx{
				ChainID:   t.ChainID,
				Nonce:     t.Nonce,
				GasTipCap: t.GasTipCap,
				GasFeeCap: t.GasFeeCap,
				GasLimit:  t.GasLimit,
				To:        t.To,
				Amount:    t.Amount,
				Data:      t.Data,
				Accesses:  accesses(t.Accesses),
				V:         t.V,
				R:         t.R,
				S:         t.S,
			}

		default:
			return nil, fmt.Errorf("unsupported legacy tx data type for conversion: %T", t)
		}

		anyData, err := codectypes.NewAnyWithValue(txData)
		if err != nil {
			return nil, fmt.Errorf("failed to pack new evm TxData into Any from ethermint: %w", err)
		}

		return &evmtypes.MsgEthereumTx{
			Data:  anyData,
			Size_: m.Size_,
			Hash:  m.Hash,
			From:  m.From,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported message type: %T", m)
	}
}
// accesses converts an etherminttypes.AccessList to an evmtypes.AccessList and returns it.
func accesses(legacyAccesses etherminttypes.AccessList) evmtypes.AccessList {
	if legacyAccesses == nil {
		return nil
	}

	newAccesses := make(evmtypes.AccessList, len(legacyAccesses))
	for i, tuple := range legacyAccesses {
		newAccesses[i] = evmtypes.AccessTuple{
			Address:     tuple.Address,
			StorageKeys: tuple.StorageKeys,
		}
	}
	return newAccesses
}
