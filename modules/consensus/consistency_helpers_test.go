package consensus

import (
	"github.com/threefoldtech/rivine/crypto"

	"github.com/rivine/bbolt"
)

// dbConsensusChecksum is a convenience function to call consensusChecksum
// without a bolt.Tx.
func (cs *ConsensusSet) dbConsensusChecksum() (checksum crypto.Hash) {
	err := cs.db.Update(func(tx *bolt.Tx) error {
		checksum = consensusChecksum(tx)
		return nil
	})
	if err != nil {
		panic(err)
	}
	return checksum
}
