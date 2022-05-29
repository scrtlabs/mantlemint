package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/server"
	serverConfig "github.com/cosmos/cosmos-sdk/server/config"
	servergrpc "github.com/cosmos/cosmos-sdk/server/grpc"
	"github.com/cosmos/cosmos-sdk/server/grpc/gogoreflection"
	"github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	scrt "github.com/enigmampc/SecretNetwork/app"
	core "github.com/enigmampc/SecretNetwork/types"
	"github.com/enigmampc/SecretNetwork/x/compute"
	"github.com/gorilla/mux"
	blockFeeder "github.com/scrtlabs/mantlemint/block_feed"
	"github.com/scrtlabs/mantlemint/config"
	"github.com/scrtlabs/mantlemint/db/heleveldb"
	"github.com/scrtlabs/mantlemint/db/hld"
	"github.com/scrtlabs/mantlemint/db/safe_batch"
	"github.com/scrtlabs/mantlemint/indexer"
	"github.com/scrtlabs/mantlemint/indexer/block"
	"github.com/scrtlabs/mantlemint/indexer/tx"
	"github.com/scrtlabs/mantlemint/mantlemint"
	"github.com/scrtlabs/mantlemint/rpc"
	"github.com/scrtlabs/mantlemint/store/rootmulti"
	"github.com/spf13/cast"
	"github.com/spf13/viper"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proxy"
	tendermint "github.com/tendermint/tendermint/types"
	"google.golang.org/grpc"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	tmdb "github.com/tendermint/tm-db"
)

// initialize mantlemint for v0.34.x
func main() {
	mantlemintConfig := config.NewConfig()
	mantlemintConfig.Print()

	sdkConfig := sdk.GetConfig()
	sdkConfig.SetCoinType(core.CoinType)
	// SetFullFundraiserPath is a deprecated function
	//sdkConfig.SetFullFundraiserPath(core.FullFundraiserPath)
	sdkConfig.SetBech32PrefixForAccount(core.Bech32PrefixAccAddr, core.Bech32PrefixAccPub)
	sdkConfig.SetBech32PrefixForValidator(core.Bech32PrefixValAddr, core.Bech32PrefixValPub)
	sdkConfig.SetBech32PrefixForConsensusNode(core.Bech32PrefixConsAddr, core.Bech32PrefixConsPub)
	sdkConfig.SetAddressVerifier(core.AddressVerifier)
	sdkConfig.Seal()

	ldb, ldbErr := heleveldb.NewLevelDBDriver(&heleveldb.DriverConfig{
		Name: mantlemintConfig.MantlemintDB,
		Dir:  mantlemintConfig.Home,
		Mode: heleveldb.DriverModeKeySuffixDesc,
	})
	if ldbErr != nil {
		panic(ldbErr)
	}

	var hldb = hld.ApplyHeightLimitedDB(
		ldb,
		&hld.HeightLimitedDBConfig{
			Debug: true,
		},
	)

	batched := safe_batch.NewSafeBatchDB(hldb)
	batchedOrigin := batched.(safe_batch.SafeBatchDBCloser)
	logger := tmlog.NewTMLogger(os.Stdout)
	codec := scrt.MakeEncodingConfig()

	// customize CMS to limit kv store's read height on query
	cms := rootmulti.NewStore(batched, hldb)
	vpr := viper.GetViper()

	wasmConfig := compute.DefaultWasmConfig()
	wasmConfig.EnclaveCacheSize = uint8(100)

	var (
		err error
	)

	pruningOpts, err := server.GetPruningOptionsFromFlags(vpr)
	if err != nil {
		panic(err)
	}

	var app = scrt.NewSecretNetworkApp(
		logger,
		batched,
		nil,
		true, // need this so KVStores are set
		make(map[int64]bool),
		mantlemintConfig.Home,
		0,
		false,
		vpr,
		wasmConfig,
		fauxMerkleModeOpt,
		func(ba *baseapp.BaseApp) {
			ba.SetCMS(cms)
		},
		baseapp.SetPruning(pruningOpts),
		baseapp.SetMinGasPrices(cast.ToString(vpr.Get(server.FlagMinGasPrices))),
		baseapp.SetHaltHeight(cast.ToUint64(vpr.Get(server.FlagHaltHeight))),
		baseapp.SetHaltTime(cast.ToUint64(vpr.Get(server.FlagHaltTime))),
		baseapp.SetMinRetainBlocks(cast.ToUint64(vpr.Get(server.FlagMinRetainBlocks))),
		baseapp.SetTrace(cast.ToBool(vpr.Get(server.FlagTrace))),
		baseapp.SetIndexEvents(cast.ToStringSlice(vpr.Get(server.FlagIndexEvents))),
		baseapp.SetSnapshotInterval(cast.ToUint64(vpr.Get(server.FlagStateSyncSnapshotInterval))),
		baseapp.SetSnapshotKeepRecent(cast.ToUint32(vpr.Get(server.FlagStateSyncSnapshotKeepRecent))),
	)

	// create app...
	var appCreator = mantlemint.NewConcurrentQueryClientCreator(app)
	appConns := proxy.NewAppConns(appCreator)
	appConns.SetLogger(logger)
	if startErr := appConns.OnStart(); startErr != nil {
		panic(startErr)
	}

	go func() {
		a := <-appConns.Quit()
		fmt.Println(a)
	}()

	var executor = mantlemint.NewMantlemintExecutor(batched, appConns.Consensus())
	var mm = mantlemint.NewMantlemint(
		batched,
		appConns,
		executor,

		// run before
		nil,

		// RunAfter Inject callback
		nil,
	)

	// initialize using provided genesis
	genesisDoc := getGenesisDoc(mantlemintConfig.GenesisPath)
	initialHeight := genesisDoc.InitialHeight

	// set target initial write height to genesis.initialHeight;
	// this is safe as upon Inject it will be set with block.Height
	hldb.SetWriteHeight(initialHeight)
	batchedOrigin.Open()

	// initialize state machine with genesis
	if initErr := mm.Init(genesisDoc); initErr != nil {
		panic(initErr)
	}

	// flush to db; panic upon error (can't proceed)
	if rollback, flushErr := batchedOrigin.Flush(); flushErr != nil {
		debug.PrintStack()
		panic(flushErr)
	} else if rollback != nil {
		rollback.Close()
	}

	// load initial state to mantlemint
	if loadErr := mm.LoadInitialState(); loadErr != nil {
		panic(loadErr)
	}

	// initialization is done; clear write height
	hldb.ClearWriteHeight()

	// get blocks over some sort of transport, inject to mantlemint
	blockFeed := blockFeeder.NewAggregateBlockFeed(
		mm.GetCurrentHeight(),
		mantlemintConfig.RPCEndpoints,
		mantlemintConfig.WSEndpoints,
	)

	// create indexer service
	indexerInstance, indexerInstanceErr := indexer.NewIndexer("indexer", mantlemintConfig.Home)
	if indexerInstanceErr != nil {
		panic(indexerInstanceErr)
	}

	indexerInstance.RegisterIndexerService("tx", tx.IndexTx)
	indexerInstance.RegisterIndexerService("block", block.IndexBlock)

	abcicli, _ := appCreator.NewABCIClient()
	rpccli := rpc.NewRpcClient(abcicli)

	// rest cache invalidate channel
	cacheInvalidateChan := make(chan int64)

	// start RPC server
	rpcErr := rpc.StartRPC(
		app,
		rpccli,
		mantlemintConfig.ChainID,
		codec,
		cacheInvalidateChan,

		// callback for registering custom routers; primarily for indexers
		// default: noop,
		// todo: make this part injectable
		func(router *mux.Router) {
			indexerInstance.RegisterRESTRoute(router, tx.RegisterRESTRoute)
			indexerInstance.RegisterRESTRoute(router, block.RegisterRESTRoute)
		},

		// inject flag checker for synced
		blockFeed.IsSynced,
	)

	if rpcErr != nil {
		panic(rpcErr)
	}

	var (
		grpcSrv    *grpc.Server
		grpcWebSrv *http.Server
	)

	config := serverConfig.GetConfig(vpr)

	if config.GRPC.Enable {
		grpcSrv, err = StartGRPCServer(app, config.GRPC.Address)
		if err != nil {
			panic(err)
		}

		if config.GRPCWeb.Enable {
			grpcWebSrv, err = servergrpc.StartGRPCWeb(grpcSrv, config)
			if err != nil {
				panic(err)
			}
		}
	}

	// start subscribing to block
	if mantlemintConfig.DisableSync {
		fmt.Println("running without sync...")
		forever()
	} else if cBlockFeed, blockFeedErr := blockFeed.Subscribe(0); blockFeedErr != nil {
		panic(blockFeedErr)
	} else {
		var rollbackBatch tmdb.Batch
		for {
			feed := <-cBlockFeed

			// open db batch
			hldb.SetWriteHeight(feed.Block.Height)
			batchedOrigin.Open()
			if injectErr := mm.Inject(feed.Block); injectErr != nil {
				// rollback last block
				if rollbackBatch != nil {
					fmt.Println("rollback previous block")
					rollbackBatch.WriteSync()
					rollbackBatch.Close()
				}

				debug.PrintStack()
				panic(injectErr)
			}

			// last block is okay -> dispose rollback batch
			if rollbackBatch != nil {
				rollbackBatch.Close()
				rollbackBatch = nil
			}

			// run indexer BEFORE batch flush
			if indexerErr := indexerInstance.Run(feed.Block, feed.BlockID, mm.GetCurrentEventCollector()); indexerErr != nil {
				debug.PrintStack()
				panic(indexerErr)
			}

			// flush db batch
			// returns rollback batch that reverts current block injection
			if rollback, flushErr := batchedOrigin.Flush(); flushErr != nil {
				debug.PrintStack()
				panic(flushErr)
			} else {
				rollbackBatch = rollback
			}

			hldb.ClearWriteHeight()

			cacheInvalidateChan <- feed.Block.Height
		}
	}

	defer func() {
		if grpcSrv != nil {
			grpcSrv.Stop()
			if grpcWebSrv != nil {
				grpcWebSrv.Close()
			}
		}
	}()
}

// Pass this in as an option to use a dbStoreAdapter instead of an IAVLStore for simulation speed.
func fauxMerkleModeOpt(app *baseapp.BaseApp) {
	app.SetFauxMerkleMode()
}

func getGenesisDoc(genesisPath string) *tendermint.GenesisDoc {
	jsonBlob, _ := ioutil.ReadFile(genesisPath)
	shasum := sha1.New()
	shasum.Write(jsonBlob)
	sum := hex.EncodeToString(shasum.Sum(nil))

	log.Printf("[v0.34.x/sync] genesis shasum=%s", sum)

	if genesis, genesisErr := tendermint.GenesisDocFromFile(genesisPath); genesisErr != nil {
		panic(genesisErr)
	} else {
		return genesis
	}
}

func StartGRPCServer(app types.Application, address string) (*grpc.Server, error) {
	grpcSrv := grpc.NewServer()
	app.RegisterGRPCServer(grpcSrv)
	// reflection allows consumers to build dynamic clients that can write
	// to any cosmos-sdk application without relying on application packages at compile time
	// Reflection allows external clients to see what services and methods
	// the gRPC server exposes.
	gogoreflection.Register(grpcSrv)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}

	errCh := make(chan error)
	go func() {
		err = grpcSrv.Serve(listener)
		if err != nil {
			errCh <- fmt.Errorf("failed to serve: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return nil, err
	case <-time.After(types.ServerStartTime): // assume server started successfully
		return grpcSrv, nil
	}
}

func forever() {
	<-(chan int)(nil)
}
