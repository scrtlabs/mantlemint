package main

import (
	"github.com/spf13/viper"
	wasmconfig "github.com/terra-money/core/x/wasm/config"
	wasmtypes "github.com/terra-money/core/x/wasm/types"
	"github.com/terra-money/mantlemint/config"
	. "github.com/terra-money/mantlemint/lib"
	"path/filepath"
)

func main() {
	config := config.NewConfig()
	v := viper.GetViper()
	wasmConfig := wasmconfig.GetConfig(v)

	rebuildErr := RebuildWasmCache(
		filepath.Join(config.Home, wasmconfig.DBDir),
		wasmtypes.DefaultFeatures,
		wasmConfig.ContractMemoryCacheSize,
	)

	if rebuildErr != nil {
		panic(rebuildErr)
	}
}
