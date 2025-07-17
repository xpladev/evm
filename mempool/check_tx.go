package mempool

import (
	"errors"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func NewCheckTxHandler(mempool *EVMMempool) types.CheckTxHandler {
	return func(runTx types.RunTx, request *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
		gInfo, result, anteEvents, err := runTx(request.Tx, nil)
		if err != nil {
			// detect if there is a sequence error
			if errors.Is(err, sdkerrors.ErrInvalidSequence) {
				// send it to the mempool for further triage
				err := mempool.InsertInvalidSequence(request.Tx)
				if err != nil {
					return sdkerrors.ResponseCheckTxWithEvents(err, gInfo.GasWanted, gInfo.GasUsed, anteEvents, false), nil
				}
			}
			// anything else, return regular error
			return sdkerrors.ResponseCheckTxWithEvents(err, gInfo.GasWanted, gInfo.GasUsed, anteEvents, false), nil
		}

		return &abci.ResponseCheckTx{
			GasWanted: int64(gInfo.GasWanted),
			GasUsed:   int64(gInfo.GasUsed),
			Log:       result.Log,
			Data:      result.Data,
			Events:    types.MarkEventsToIndex(result.Events, nil),
		}, nil
	}
}
