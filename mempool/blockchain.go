package mempool

import (
	errors2 "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/evm/mempool/txpool"
	"github.com/cosmos/evm/mempool/txpool/legacypool"
	"github.com/cosmos/evm/x/vm/statedb"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/params"
	"math"
	"math/big"
)

var _ txpool.BlockChain = Blockchain{}
var _ legacypool.BlockChain = Blockchain{}

type Blockchain struct {
	ctx                func(height int64, prove bool) (sdk.Context, error)
	vmKeeper           VMKeeperI
	feeMarketKeeper    FeeMarketKeeperI
	chainHeadFeed      *event.Feed
	zeroHeader         *types.Header
	previousHeaderHash common.Hash
}

func NewBlockchain(ctx func(height int64, prove bool) (sdk.Context, error), vmKeeper VMKeeperI, feeMarketKeeper FeeMarketKeeperI) *Blockchain {
	return &Blockchain{
		ctx:             ctx,
		vmKeeper:        vmKeeper,
		feeMarketKeeper: feeMarketKeeper,
		chainHeadFeed:   new(event.Feed),
		zeroHeader: &types.Header{
			Number: big.NewInt(0),
		},
	}
}

func (b Blockchain) Config() *params.ChainConfig {
	return evmtypes.GetEthChainConfig()
}

func (b Blockchain) CurrentBlock() *types.Header {
	ctx, err := b.GetLatestCtx()
	if err != nil {
		//todo: handle proper error
		return b.zeroHeader
	}

	consParams := ctx.ConsensusParams()
	gasLimit := sdkmath.NewIntFromUint64(math.MaxUint64)

	// NOTE: a MaxGas equal to -1 means that block gas is unlimited
	if consParams.Block != nil && consParams.Block.MaxGas > -1 {
		gasLimit = sdkmath.NewInt(consParams.Block.MaxGas)
	}

	// todo: make sure that the base fee calculation and parameters here are correct.
	header := &types.Header{
		Number:     big.NewInt(ctx.BlockHeight()),
		Time:       uint64(ctx.BlockTime().Unix()),
		GasLimit:   gasLimit.Uint64(),
		GasUsed:    b.feeMarketKeeper.GetBlockGasWanted(ctx),
		ParentHash: b.previousHeaderHash,
		Root:       common.BytesToHash(ctx.BlockHeader().AppHash), // we actually don't care that this isn't the ctx header, as long as we properly track roots and parent roots to prevent the reorg from triggering
		Difficulty: big.NewInt(0),                                 // 0 difficulty on PoS
	}

	chainConfig := evmtypes.GetEthChainConfig()
	if chainConfig.IsLondon(header.Number) {
		baseFee := b.vmKeeper.GetBaseFee(ctx)
		if baseFee != nil {
			header.BaseFee = baseFee
		}
	}

	return header
}

func (b Blockchain) GetBlock(_ common.Hash, _ uint64) *types.Block {
	// For instant finality chains, reorgs never happen, so this method should never be called.
	// If it is called, it indicates a bug in the mempool logic or an incorrect assumption.
	currBlock := b.CurrentBlock()
	if currBlock.Number.Cmp(big.NewInt(0)) == 0 {
		currBlock.ParentHash = common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000")
		return types.NewBlockWithHeader(currBlock)
	} else if currBlock.Number.Cmp(big.NewInt(1)) == 0 {
		return types.NewBlockWithHeader(currBlock)
	}

	panic("GetBlock should never be called on a Cosmos chain due to instant finality - this indicates a reorg is being attempted")
}

func (b Blockchain) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.chainHeadFeed.Subscribe(ch)
}

// NotifyNewBlock sends a chain head event when a new block is finalized
func (b *Blockchain) NotifyNewBlock() {
	// Send the chain head event
	header := b.CurrentBlock()
	b.chainHeadFeed.Send(core.ChainHeadEvent{Header: header})
	b.previousHeaderHash = header.Hash()
}

func (b Blockchain) StateAt(hash common.Hash) (vm.StateDB, error) {
	// on zero block and on ctx error
	if hash == common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000") || hash == types.EmptyCodeHash {
		return vm.StateDB(nil), nil
	}
	ctx, err := b.GetLatestCtx()
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	return statedb.New(ctx, b.vmKeeper, statedb.NewEmptyTxConfig(common.Hash(ctx.BlockHeader().AppHash))), nil
}

func (b Blockchain) GetLatestCtx() (sdk.Context, error) {
	ctx, err := b.ctx(0, false)
	if err != nil {
		return sdk.Context{}, errors2.Wrapf(err, "getting latest context")
	}
	return ctx, nil
}
