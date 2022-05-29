package height

import (
	"fmt"
	"github.com/scrtlabs/mantlemint/indexer"
	"github.com/scrtlabs/mantlemint/mantlemint"
	tmjson "github.com/tendermint/tendermint/libs/json"
	tm "github.com/tendermint/tendermint/types"
	tmdb "github.com/tendermint/tm-db"
)

var IndexHeight = indexer.CreateIndexer(func(indexerDB tmdb.Batch, block *tm.Block, _ *tm.BlockID, _ *mantlemint.EventCollector) error {
	defer fmt.Printf("[indexer/height] indexing done for height %d\n", block.Height)
	height := block.Height

	record := HeightRecord{Height: uint64(height)}
	recordJSON, recordErr := tmjson.Marshal(record)
	if recordErr != nil {
		return recordErr
	}

	return indexerDB.Set(getKey(), recordJSON)
})
