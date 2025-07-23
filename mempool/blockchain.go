package mempool

import (
	"errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/evm/mempool/txpool"
	"github.com/cosmos/evm/mempool/txpool/legacypool"
	vmkeeper "github.com/cosmos/evm/x/vm/keeper"
	"github.com/cosmos/evm/x/vm/statedb"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/params"
	"math/big"
)

var _ txpool.BlockChain = Blockchain{}
var _ legacypool.BlockChain = Blockchain{}

type Blockchain struct {
	ctx      func(height int64, prove bool) (sdk.Context, error)
	vmKeeper vmkeeper.Keeper
}

func NewBlockchain(ctx func(height int64, prove bool) (sdk.Context, error), vmKeeper vmkeeper.Keeper) *Blockchain {
	return &Blockchain{
		ctx:      ctx,
		vmKeeper: vmKeeper,
	}
}

func (b Blockchain) Config() *params.ChainConfig {
	return evmtypes.GetEthChainConfig()
}

func (b Blockchain) CurrentBlock() *types.Header {
	ctx, err := b.GetLatestCtx()
	if err != nil {
		return nil
	}

	return &types.Header{
		Number:   big.NewInt(ctx.BlockHeight()),
		Time:     uint64(ctx.BlockTime().Unix()),
		GasLimit: ctx.BlockGasMeter().Limit(),
		Root:     common.BytesToHash(ctx.HeaderHash()),
	}
}

func (b Blockchain) GetBlock(_ common.Hash, _ uint64) *types.Block {
	// For instant finality chains, reorgs never happen, so this method should never be called.
	// If it is called, it indicates a bug in the mempool logic or an incorrect assumption.
	panic("GetBlock should never be called on instant finality chains - this indicates a reorg is being attempted")
}

func (b Blockchain) SubscribeChainHeadEvent(_ chan<- core.ChainHeadEvent) event.Subscription {
	panic("todo")
}

func (b Blockchain) StateAt(_ common.Hash) (vm.StateDB, error) {
	ctx, err := b.GetLatestCtx()
	if err != nil {
		return nil, err
	}
	return statedb.New(ctx, &b.vmKeeper, statedb.NewEmptyTxConfig(common.Hash(ctx.HeaderHash()))), nil
}

func (b Blockchain) GetLatestCtx() (sdk.Context, error) {
	ctx, err := b.ctx(0, false)
	if err != nil {
		return sdk.Context{}, errors.New("failed to get latest context")
	}
	return ctx, nil
}
