package consensus

import (
	"math/big"
	"sync"
)

var (
	ZeroAddress  = CoinAddress{0}
	ZeroCurrency = NewCurrency64(0)
)

type (
	BlockWeight *big.Rat
)

// The State is the object responsible for tracking the current status of the
// blockchain. It accepts blocks and maintains an understanding of competing
// forks. The State object is responsible for maintaining consensus.
type State struct {
	// The blockRoot is the block node that contains the genesis block, which
	// is the foundation for all other blocks. blockNodes form a tree, each
	// having many children and pointing back to the parent.
	blockRoot *blockNode

	// Maps tracking blocks that have been seen. badBlocks is a list of blocks
	// that are somehow invalid (such as containing invalid transactions). The
	// second is a list of valid blocks which have nodes in the State.
	badBlocks map[BlockID]struct{}
	blockMap  map[BlockID]*blockNode

	// currentPath and currentBlockID track which blocks are currently accepted
	// as the longest known blockchain.
	currentBlockID BlockID
	currentPath    map[BlockHeight]BlockID

	// These are the consensus variables. All nodes on the network which have
	// the same path will have the same exact consensus variables, anything
	// else is a software bug.
	siafundPool           Currency
	unspentSiacoinOutputs map[OutputID]SiacoinOutput
	openFileContracts     map[FileContractID]FileContract
	unspentSiafundOutputs map[OutputID]SiafundOutput
	delayedSiacoinOutputs map[BlockHeight]map[OutputID]SiacoinOutput

	// Per convention, all exported functions in the consensus package can be
	// called concurrently. The state mutex helps to orchestrate thread safety.
	// To keep things simple, the entire state was chosen to have a single
	// mutex, as opposed to putting frequently accessed fields under separate
	// mutexes. The performance advantage was decided to be not worth the
	// complexity tradeoff.
	mu sync.RWMutex
}

// CreateGenesisState will create the state that contains the genesis block and
// nothing else. genesisTime is taken as an input instead of the constant being
// used directly because it makes certain parts of testing a lot easier.
func CreateGenesisState(genesisTime Timestamp) (s *State) {
	// Create a new state and initialize the maps.
	s = &State{
		badBlocks:             make(map[BlockID]struct{}),
		blockMap:              make(map[BlockID]*blockNode),
		currentPath:           make(map[BlockHeight]BlockID),
		unspentSiacoinOutputs: make(map[OutputID]SiacoinOutput),
		openFileContracts:     make(map[FileContractID]FileContract),
		unspentSiafundOutputs: make(map[OutputID]SiafundOutput),
		delayedSiacoinOutputs: make(map[BlockHeight]map[OutputID]SiacoinOutput),
	}

	// Create the genesis block and add it as the BlockRoot.
	genesisBlock := Block{
		Timestamp: genesisTime,
	}
	s.blockRoot = &blockNode{
		block:  genesisBlock,
		target: RootTarget,
		depth:  RootDepth,
	}
	s.blockMap[genesisBlock.ID()] = s.blockRoot

	// Fill out the consensus informaiton for the genesis block.
	s.currentBlockID = genesisBlock.ID()
	s.currentPath[BlockHeight(0)] = genesisBlock.ID()
	s.unspentSiacoinOutputs[genesisBlock.MinerPayoutID(0)] = SiacoinOutput{
		Value:     CalculateCoinbase(0),
		SpendHash: ZeroAddress, // TODO: change to Nebulous Genesis Siacoin SpendHash Address
	}
	s.unspentSiafundOutputs[OutputID{0}] = SiafundOutput{
		Value:            NewCurrency64(SiafundCount),
		SpendHash:        ZeroAddress, // TODO: change to Nebulous Genesis Siafund SpendHash Address
		ClaimDestination: ZeroAddress, // TODO: change to Nebulous Genesis ClaimDestination Address
	}

	return
}

// RLock will readlock the state.
//
// TODO: Add a safety timer which will auto-unlock if the readlock is held for
// more than a second. (panic in debug mode)
func (s *State) RLock() {
	s.mu.RLock()
}

// RUnlock will readunlock the state.
//
// TODO: when the safety timer is added to RLock, add a timer disabler to
// RUnlock to prevent too many unlocks from being called.
func (s *State) RUnlock() {
	s.mu.RUnlock()
}
