package lib

import (
	"encoding/hex"
	wasm "github.com/CosmWasm/wasmvm/api"
	wasmconfig "github.com/terra-money/core/x/wasm/config"
	"github.com/terra-money/core/x/wasm/types"
	"io/ioutil"
	"log"
	"path/filepath"
)

// RebuildWasmCache loads everything under dataDir and compile it by calling GetCode
func RebuildWasmCache(
	dataDir string,
	supportedFeatures string,
	cacheSize uint32,
) error {
	blobs, err := ioutil.ReadDir(filepath.Join(dataDir, "state/wasm"))

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
		blobBytes, decodeErr := hex.DecodeString(blobName)
		if decodeErr != nil {
			return err
		}

		log.Printf("recompiling wasm blob %s..\n", blobName)
		_, analyzeErr := wasm.AnalyzeCode(cache, blobBytes)

		if analyzeErr != nil {
			return analyzeErr
		}
	}

	return nil

}
