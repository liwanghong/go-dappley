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
	}

	pool.forkTails, _ = lru.NewWithEvict(BlockPoolForkChainLimit, func(key interface{}, value interface{}) {
		logger.Debugf("Remove block %v", value.(*Block).GetHash())
		if pool.skipEvict == false {
			pool.removeOldForkTail(key.(string))
		}
	})
	pool.longestTailHash = nil
	pool.skipEvict = false
	pool.forkBlocks = make(map[string]*ForkBlock)
	return pool
}

func (pool *BlockPool) SetBlockchain(bc *Blockchain) {
	pool.bc = bc
}

func (pool *BlockPool) BlockRequestCh() chan BlockRequestPars {
	return pool.blockRequestCh
}

func (pool *BlockPool) GetBlockchain() *Blockchain {
	return pool.bc
}

//Verify all transactions in a fork
func (pool *BlockPool) VerifyTransactions(utxo UTXOIndex, forkBlks []*Block) bool {
	for i := len(forkBlks) - 1; i >= 0; i-- {
		logger.Info("Start Verify")
		if !forkBlks[i].VerifyTransactions(utxo) {
			return false
		}
		logger.Info("Verifyed a block. Height: ", forkBlks[i].GetHeight(), "Have ", i, "block left")
		utxoIndex := LoadUTXOIndex(pool.bc.GetDb())
		utxoIndex.BuildForkUtxoIndex(forkBlks[i], pool.bc.GetDb())
	}
	return true
}


func (pool *BlockPool) Push(block *Block, pid peer.ID) {
	logger.Debug("BlockPool: Has received a new block")

	if !block.VerifyHash() {
		logger.Debug("BlockPool: Verify Hash failed!")
		return
	}

	if !(pool.bc.GetConsensus().VerifyBlock(block)) {
		logger.Debug("BlockPool: Verify Signature failed!")
		return
	}
	//TODO: Verify double spending transactions in the same block

	logger.Debug("BlockPool: Block has been verified")
	pool.handleRecvdBlock(block, pid)
}

func (pool *BlockPool) handleRecvdBlock(blk *Block, sender peer.ID)  {
	logger.Debugf("BlockPool: Received a new block: %v", blk.GetHash(), " From Sender: ", sender.String())

	if pool.bc.IsInBlockchain(blk.GetHash()) {
		logger.Debugf("BlockPool: Blockchain already contains blk: %v", blk.GetHash(), " returning")
		return
	}

	//Block in forkBlocks
	existForkBlock, ok := pool.forkBlocks[blk.HashString()]
	if ok {
		if existForkBlock.forkState == ForkBlockReady {
			logger.Debugf("BlockPool: Fork Pool already contains blk: ", blk.GetHash())
		return
	}
	}

	//TODO: verify
	if   true {
		logger.Debugf("BlockPool: Adding node key to bpcache: %v", blk.GetHash())
	}else{
		logger.Debug("BlockPool: Block: ", blk.HashString(), " did not pass verification process, discarding block")
		return
	}

	pool.addBlock2Pool(blk, sender)
	//attach above partial tree to forktree
	if pool.isForkCanMerge() == true {
		//build forkchain based on highest leaf in tree
		forkPool := pool.updateForkPool()
		//merge forkchain into blockchain
		pool.bc.MergeFork(forkPool)
	}
}

func (pool *BlockPool) addBlock2Pool(blk *Block, sender peer.ID) {
	if pool.forkTails.Contains(string(blk.GetPrevHash())) {
		//Remove the new block's parent from forkTail
		pool.skipEvict = true
		pool.forkTails.Remove(string(blk.GetPrevHash()))
		pool.skipEvict = false
	}

	existForkBlock, ok := pool.forkBlocks[blk.HashString()]
	if ok {
		//New block is exist block's parent block
		logger.Debugf("Add block to expect node %v", blk.GetHash())
		existForkBlock.forkState = ForkBlockReady
		existForkBlock.block = blk
	} else {
		//New block is fork tail block
		logger.Debugf("Add block to tail %v", blk.GetHash())
		pool.forkTails.Add(blk.HashString(), blk)
		pool.forkBlocks[blk.HashString()] = &ForkBlock{ForkBlockReady, 0, blk}

		lastLongestForkBlock, ok := pool.forkBlocks[string(pool.longestTailHash)]
		if ok == false || lastLongestForkBlock.block.GetHeight() < blk.GetHeight() {
			pool.longestTailHash = blk.GetHash()
		}
	}

	pool.checkAndRequestBlock(blk.GetPrevHash(), sender)
}

// check block in blockchain or forkBlocks, If not in blockchain or forkBlocks, request it from sender
func (pool *BlockPool) checkAndRequestBlock(hash Hash, sender peer.ID) {
	if pool.bc.IsInBlockchain(hash) {
		return
	}

	existForkBlock, ok := pool.forkBlocks[string(hash)]
	if ok {
			existForkBlock.childrenCount++
		}else{
			pool.forkBlocks[string(hash)] = &ForkBlock{ForkBlockExpect, 1, nil}
			pool.requestBlock(hash, sender)
		}
	}

func (pool *BlockPool) isForkCanMerge() bool {
	if pool.longestTailHash == nil {
		return false
	}

	tailBlockValue, ok := pool.forkTails.Get(string(pool.longestTailHash))
	if ok != true {
		logger.Errorf("ERROR: tailHash not in forkTail Cache %v", pool.longestTailHash)
		return false
	}

	tailBlock := tailBlockValue.(*Block)
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

func (pool *BlockPool) updateForkPool() []*Block {
	blockValue, _ := pool.forkTails.Get(string(pool.longestTailHash))
	block := blockValue.(*Block)
	var forkPool []*Block  
	for {
		delete(pool.forkBlocks, block.HashString())
		forkPool = append(forkPool, block)

		if pool.bc.IsInBlockchain(block.GetPrevHash()) {
			break
		}

		prevBlock, _ := pool.forkBlocks[string(block.GetPrevHash())]
		block = prevBlock.block
	}

	pool.skipEvict = true
	pool.forkTails.Remove(string(pool.longestTailHash))
	pool.skipEvict = false
	pool.longestTailHash = nil
	pool.refreshLongestTailHash()
	return forkPool
}

func (pool *BlockPool) refreshLongestTailHash() {
	var maxHeight uint64
	var longestHash Hash
	for _, key := range pool.forkTails.Keys() {
		blockValue, _ := pool.forkTails.Get(key)
		block := blockValue.(*Block)

		if block.GetHeight() > maxHeight {
			maxHeight = block.GetHeight()
			longestHash = block.GetHash()
		}
	}
    pool.longestTailHash = longestHash
}

func (pool *BlockPool) removeOldForkTail(hashString string) {
	forkBlock, ok := pool.forkBlocks[hashString]
	for ok {
		forkBlock.childrenCount--
		if forkBlock.forkState == ForkBlockExpect {
			delete(pool.forkBlocks, hashString)
			break
		} else if forkBlock.childrenCount <= 0 {
			delete(pool.forkBlocks, hashString)
			logger.Debugf("Remove old fork tail: %v", forkBlock.block.GetHash())
			forkBlock, ok = pool.forkBlocks[string(forkBlock.block.GetPrevHash())]
		} else {
			break
		}
	}
}

//TODO: RequestChannel should be in PoW.go
func (pool *BlockPool) requestBlock(hash Hash, pid peer.ID) {
	pool.blockRequestCh <- BlockRequestPars{hash, pid}
}

