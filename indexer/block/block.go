package block

import (
	"fmt"
	"github.com/scrtlabs/mantlemint/indexer"
	"github.com/scrtlabs/mantlemint/mantlemint"
	tmjson "github.com/tendermint/tendermint/libs/json"
	tm "github.com/tendermint/tendermint/types"
	tmdb "github.com/tendermint/tm-db"
)

var IndexBlock = indexer.CreateIndexer(func(indexerDB tmdb.Batch, block *tm.Block, blockID *tm.BlockID, _ *mantlemint.EventCollector) error {
	defer fmt.Printf("[indexer/block] indexing done for height %d\n", block.Height)
	record := BlockRecord{
		Block:   block,
		BlockID: blockID,
	}

	recordJSON, recordErr := tmjson.Marshal(record)
	if recordErr != nil {
		return recordErr
	}

	return indexerDB.Set(getKey(uint64(block.Height)), recordJSON)
})
