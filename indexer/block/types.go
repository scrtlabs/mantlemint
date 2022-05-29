package block

import (
	"github.com/scrtlabs/mantlemint/lib"
	tm "github.com/tendermint/tendermint/types"
)

var prefix = []byte("block/height:")
var getKey = func(height uint64) []byte {
	return lib.ConcatBytes(prefix, lib.UintToBigEndian(height))
}

type BlockRecord struct {
	BlockID *tm.BlockID `json:"block_id""`
	Block   *tm.Block   `json:"block"`
}
