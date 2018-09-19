// Copyright (C) 2018 go-dappley authors
//
// This file is part of the go-dappley library.
//
// the go-dappley library is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// the go-dappley library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with the go-dappley library.  If not, see <http://www.gnu.org/licenses/>.
//

package core

import (
	"github.com/hashicorp/golang-lru"
	"github.com/libp2p/go-libp2p-peer"
	logger "github.com/sirupsen/logrus"
)

const BlockPoolLRUCacheLimit = 128
const BlockPoolForkChainLimit = 10

type BlockRequestPars struct {
	BlockHash Hash
	Pid       peer.ID
}

type RcvedBlock struct {
	Block *Block
	Pid   peer.ID
}

type ForkBlockState uint

const (
	ForkBlockReady ForkBlockState = iota
	ForkBlockExpect
)

type ForkBlock struct {
	forkState     ForkBlockState
	childrenCount int
	block         *Block
}

type BlockPool struct {
	blockRequestCh chan BlockRequestPars
	size           int
	bc             *Blockchain
	forkPool       []*Block

	forkTails       *lru.Cache
	longestTailHash Hash
	skipEvict       bool
	forkBlocks      map[string]*ForkBlock
}

func NewBlockPool(size int) *BlockPool {
	pool := &BlockPool{
		size:           size,
		blockRequestCh: make(chan BlockRequestPars, size),
		bc:             nil,
		forkPool:       []*Block{},
	}

	pool.forkTails, _ = lru.New(BlockPoolForkChainLimit, func(key interface{}, value interface{}) {
		if pool.skipEvict {
			pool.removeOldForkTail(key.(string))
		}
	})
	pool.longestTailHash = nil
	pool.skipEvict = false
	pool.forkBlocks = make(map[string]*Block)
	return pool
}

func (pool *BlockPool) SetBlockchain(bc *Blockchain) {
	pool.bc = bc
}

func (pool *BlockPool) BlockRequestCh() chan BlockRequestPars {
	return pool.blockRequestCh
}

func (pool *BlockPool) GetForkPool() []*Block { return pool.forkPool }

func (pool *BlockPool) ForkPoolLen() int {
	return len(pool.forkPool)
}

func (pool *BlockPool) GetForkPoolHeadBlk() *Block {
	if len(pool.forkPool) > 0 {
		return pool.forkPool[len(pool.forkPool)-1]
	}
	return nil
}

func (pool *BlockPool) GetForkPoolTailBlk() *Block {
	if len(pool.forkPool) > 0 {
		return pool.forkPool[0]
	}
	return nil
}

func (pool *BlockPool) ResetForkPool() {
	pool.forkPool = []*Block{}
}

func (pool *BlockPool) ReInitializeForkPool(blk *Block) {
	logger.Debug("Fork: Re-initilaize fork with the new block")
	pool.ResetForkPool()
	pool.forkPool = append(pool.forkPool, blk)
}

func (pool *BlockPool) IsParentOfFork(blk *Block) bool {
	if blk == nil || pool.ForkPoolLen() == 0 {
		return false
	}

	return IsParentBlock(blk, pool.GetForkPoolHeadBlk())
}

func (pool *BlockPool) IsTailOfFork(blk *Block) bool {
	if blk == nil || pool.ForkPoolLen() == 0 {
		return false
	}

	return IsParentBlock(pool.GetForkPoolTailBlk(), blk)
}

func (pool *BlockPool) GetBlockchain() *Blockchain {
	return pool.bc
}

//Verify all transactions in a fork
func (pool *BlockPool) VerifyTransactions(utxo UTXOIndex) bool {
	for i := pool.ForkPoolLen() - 1; i >= 0; i-- {
		logger.Info("Start Verify")
		if !pool.forkPool[i].VerifyTransactions(utxo) {
			return false
		}
		logger.Info("Verifyed a block. Height: ", pool.forkPool[i].GetHeight(), "Have ", i, "block left")
		utxoIndex := LoadUTXOIndex(pool.bc.GetDb())
		utxoIndex.Update(pool.forkPool[i], pool.bc.GetDb())
	}
	return true
}

func (pool *BlockPool) updateForkFromTail(blk *Block) bool {

	isTail := pool.IsTailOfFork(blk)
	if isTail {
		//only update if the block is higher than the current blockchain
		if pool.bc.IsHigherThanBlockchain(blk) {
			logger.Debug("BlockPool: Add block to tail")
			pool.addTailToForkPool(blk)
		} else {
			//if the fork's max height is less than the blockchain, delete the fork
			logger.Debug("BlockPool: Fork height too low. Dump the fork...")
			pool.ResetForkPool()
		}
	}
	return isTail
}

//returns if the operation is successful
func (pool *BlockPool) addParentToFork(blk *Block) bool {

	isParent := pool.IsParentOfFork(blk)
	if isParent {
		//check if fork's max height is still higher than the blockchain
		if pool.GetForkPoolTailBlk().GetHeight() > pool.bc.GetMaxHeight() {
			logger.Debug("BlockPool: Add block to head")
			pool.addParentToForkPool(blk)
		} else {
			//if the fork's max height is less than the blockchain, delete the fork
			logger.Debug("BlockPool: Fork height too low. Dump the fork...")
			pool.ResetForkPool()
		}
	}
	return isParent
}

func (pool *BlockPool) IsHigherThanFork(block *Block) bool {
	if block == nil {
		return false
	}
	tailBlk := pool.GetForkPoolTailBlk()
	if tailBlk == nil {
		return true
	}
	return block.GetHeight() > tailBlk.GetHeight()
}

func (pool *BlockPool) Push(block *Block, pid peer.ID) {
	logger.Debug("BlockPool: Has received a new block")

	if !block.VerifyHash() {
		logger.Info("BlockPool: Verify Hash failed!")
		return
	}

	if !(pool.bc.GetConsensus().VerifyBlock(block)) {
		logger.Warn("GetBlockPool: Verify Signature failed!")
		return
	}
	//TODO: Verify double spending transactions in the same block

	logger.Debug("BlockPool: Block has been verified")
	pool.handleRecvdBlock(block, pid)
}
func (pool *BlockPool) handleRecvdBlock(blk *Block, sender peer.ID) {
	logger.Debug("BlockPool: Received a new block: ", blk.HashString(), " From Sender: ", sender.String())

	if pool.bc.IsInBlockchain(blk.GetHash()) {
		logger.Debug("BlockPool: Blockchain already contains blk: ", blk.HashString(), " returning")
		return
	}

	//Block in forkBlocks
	existForkBlock, ok := pool.forkBlocks[blk.HashString()]
	if ok {
		if existForkBlock.forkState == ForkBlockReady {
			logger.Debug("BlockPool: Fork Pool already contains blk: ", blk.HashString())
		}
	}

	//TODO: verify
	if true {
		logger.Debug("BlockPool: Adding node key to bpcache: ", blk.HashString())
	} else {
		logger.Debug("BlockPool: Block: ", blk.HashString(), " did not pass verification process, discarding block")
		return
	}

	pool.addBlock2Pool(blk, sender)
	//attach above partial tree to forktree
	if pool.isForkCanMerge() == true {
		//build forkchain based on highest leaf in tree
		pool.updateForkPool()
		//merge forkchain into blockchain
		pool.bc.MergeFork()
	}
}

func (pool *BlockPool) addBlock2Pool(blk *Block, sender peer.ID) {
	existForkBlock, ok := pool.forkBlocks[blk.HashString()]
	if ok {
		//New block is exist block's parent block
		existForkBlock.forkState = ForkBlockReady
		existForkBlock.block = blk

		pool.checkAndRequestBlock(blk.GetPrevHash(), sender)
	} else {
		//New block is fork tail block
		pool.forkTails.Add(blk.HashString())
		pool.forkBlocks[blk.HashString()] = &ForkBlock{ForkBlockReady, 0, blk}
		if pool.forkTails.Contains(string(blk.GetPrevHash())) {
			//Remove the new block's parent from forkTail
			parentBlockFork := pool.forkBlocks[string(blk.GetPrevHash())]
			parentBlockFork.childrenCount++
			pool.skipEvict = true
			pool.forkTails.Remove(string(blk.GetPrevHash()))
			pool.skipEvict = false
		} else {
			pool.checkAndRequestBlock(blk.GetPrevHash(), sender)
		}

		lastLongestForkBlock := pool.forkBlocks[string(pool.longestTailHash)]
		if lastLongestForkBlock.block.GetHeight() < blk.GetHeight() {
			pool.longestTailHash = blk.GetHash()
		}
	}
}

// check block in blockchain or forkBlocks, If not in blockchain or forkBlocks, request it from sender
func (pool *BlockPool) checkAndRequestBlock(hash Hash, sender peer.ID) {
	if pool.bc.IsInBlockchain(hash) {
		return
	}

	existForkBlock, ok := pool.forkBlocks[string(hash)]
	if ok {
		existForkBlock.childrenCount++
	} else {
		pool.forkBlocks[string(hash)] = &ForkBlock{ForkBlockExpect, 1, nil}
		pool.requestBlock(hash, sender)
	}
}

func (pool *BlockPool) isForkCanMerge() bool {
	if pool.longestTailHash == nil {
		return false
	}

	tailBlockValue, ok := pool.forkTails.Get(string(pool.longestTailHash))
	if ok != false {
		logger.Error("ERROR: tailHash not in forkTail Cache")
		return false
	}

	tailBlock := tailBlockValue.(Block)
	if tailBlock.GetHeight() <= pool.bc.GetMaxHeight() {
		logger.Info("Fork tail is lag behind blockchain")
		return false
	}

	prevHash := tailBlock.GetPrevHash()

	for {
		if pool.bc.IsInBlockchain(prevHash) {
			return true
		}

		prevBlock, ok := pool.forkBlocks[string(prevHash)]
		if ok == false || prevBlock.forkState == ForkBlockExpect {
			return false
		}

		prevHash = prevBlock.block.GetPrevHash()
	}

	return true
}

func (pool *BlockPool) updateForkPool() {
	blockValue, _ := pool.forkTails.Get(string(pool.longestTailHash))
	block := blockValue.(Block)
	for {
		if pool.bc.IsInBlockchain(block.GetHash()) {
			break
		}

		delete(pool.forkBlocks, block.GetHash())
		pool.forkPool = append(pool.forkPool, block)

		prevBlock, _ := pool.forkBlocks[string(block.GetPrevHash())]
		block = prevBlock.block
	}

	pool.forkTails.Remove(string(pool.longestTailHash))
	pool.longestTailHash = nil
	pool.refreshLongestTailHash()
}

func (pool *BlockPool) refreshLongestTailHash() {
	maxHeight := 0
	var longestHash Hash
	for _, key := range pool.forkTails.Keys() {
		blockValue, _ := pool.forkTails.Get(key)
		block := blockValue.(Block)

		if block.GetHeight > maxHeight {
			maxHeight = block.GetHeight()
			longestHash = block.GetHash()
		}
	}

	pool.longestTailHash = longestHash
}

func (pool *BlockPool) removeOldForkTail(hashString String) {
	forkBlock, ok := pool.forkBlocks[hashString]
	for ok {
		forkBlock.childrenCount--
		if forkBlock.childrenCount <= 0 {
			delete(pool.forkBlocks, hashString)
		} else if ForkBlock.forkState == ForkBlockExpect {
			delete(pool.forkBlocks, hashString)
			break
		} else {
			break
		}

		forkBlock, ok := pool.forkBlocks[string(forkBlock.GetPrevHash())]
	}
}

//TODO: RequestChannel should be in PoW.go
func (pool *BlockPool) requestBlock(hash Hash, pid peer.ID) {
	pool.blockRequestCh <- BlockRequestPars{hash, pid}
}

func (pool *BlockPool) addTailToForkPool(blk *Block) {
	pool.forkPool = append([]*Block{blk}, pool.forkPool...)
}

func (pool *BlockPool) addParentToForkPool(blk *Block) {
	pool.forkPool = append(pool.forkPool, blk)
}
