package lib

import (
	wasm "github.com/CosmWasm/wasmvm/api"
	wasmconfig "github.com/terra-money/core/x/wasm/config"
	"github.com/terra-money/core/x/wasm/types"
	"io/ioutil"
	"log"
)

// RebuildWasmCache loads everything under dataDir and compile it by calling GetCode
func RebuildWasmCache(
	dataDir string,
	supportedFeatures string,
	cacheSize uint32,
) error {
	blobs, err := ioutil.ReadDir(dataDir)

	if err != nil {
		return err
	}

	cache, err := wasm.InitCache(
		dataDir,
		supportedFeatures,
		cacheSize,
		types.ContractMemoryLimit,
		wasmconfig.DefaultRefreshThreadNum,
	)

	for _, blob := range blobs {
		blobName := blob.Name()
		log.Printf("recompiling wasm blob %s..\n", blobName)
		_, err := wasm.AnalyzeCode(cache, []byte(blobName))

		if err != nil {
			return err
		}
	}

	return nil

}
