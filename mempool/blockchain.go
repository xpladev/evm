package mempool

import (
	"github.com/cosmos/evm/mempool/txpool"
	"github.com/cosmos/evm/mempool/txpool/legacypool"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/params"
)

var _ txpool.BlockChain = Blockchain{}
var _ legacypool.BlockChain = Blockchain{}

type Blockchain struct{}

func NewBlockchain() *Blockchain {
	return &Blockchain{}
}

func (b Blockchain) Config() *params.ChainConfig {
	//TODO implement me
	panic("implement me")
}

func (b Blockchain) CurrentBlock() *types.Header {
	//TODO implement me
	panic("implement me")
}

func (b Blockchain) GetBlock(hash common.Hash, number uint64) *types.Block {
	//TODO implement me
	panic("implement me")
}

func (b Blockchain) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	//TODO implement me
	panic("implement me")
}

func (b Blockchain) StateAt(root common.Hash) (vm.StateDB, error) {
	//TODO implement me
	panic("implement me")
}
