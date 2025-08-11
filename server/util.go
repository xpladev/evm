package server

import (
	"net"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/improbable-eng/grpc-web/go/grpcweb"
	"github.com/spf13/cobra"
	"golang.org/x/net/netutil"

	tmcmd "github.com/cometbft/cometbft/cmd/cometbft/commands"
	rpcclient "github.com/cometbft/cometbft/rpc/jsonrpc/client"

	"github.com/cosmos/evm/server/config"

	"cosmossdk.io/log"

	sdkserver "github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/version"
)

// AddCommands adds server commands
func AddCommands(
	rootCmd *cobra.Command,
	opts StartOptions,
	appExport types.AppExporter,
	addStartFlags types.ModuleInitFlags,
) {
	cometbftCmd := &cobra.Command{
		Use:     "comet",
		Aliases: []string{"cometbft"},
		Short:   "CometBFT subcommands",
	}

	cometbftCmd.AddCommand(
		sdkserver.ShowNodeIDCmd(),
		sdkserver.ShowValidatorCmd(),
		sdkserver.ShowAddressCmd(),
		sdkserver.VersionCmd(),
		tmcmd.ResetAllCmd,
		tmcmd.ResetStateCmd,
		sdkserver.BootstrapStateCmd(opts.AppCreator),
	)

	startCmd := StartCmd(opts)
	addStartFlags(startCmd)

	rootCmd.AddCommand(
		startCmd,
		cometbftCmd,
		sdkserver.ExportCmd(appExport, opts.DefaultNodeHome),
		version.NewVersionCommand(),
		sdkserver.NewRollbackCmd(opts.AppCreator, opts.DefaultNodeHome),

		// custom tx indexer command
		NewIndexTxCmd(),
	)
}

// ConnectCmtWS connects to a CometBFT WebSocket (WS) server.
// Parameters:
// - cmtRPCAddr: The RPC address of the CometBFT server.
// - cmtEndpoint: The WebSocket endpoint on the CometBFT server.
// - logger: A logger instance used to log debug and CometBFT messages.
func ConnectCmtWS(cmtRPCAddr, cmtEndpoint string, logger log.Logger) *rpcclient.WSClient {
	tmWsClient, err := rpcclient.NewWS(cmtRPCAddr, cmtEndpoint,
		rpcclient.MaxReconnectAttempts(256),
		rpcclient.ReadWait(120*time.Second),
		rpcclient.WriteWait(120*time.Second),
		rpcclient.PingPeriod(50*time.Second),
		rpcclient.OnReconnect(func() {
			logger.Debug("EVM RPC reconnects to CometBFT WS", "address", cmtRPCAddr+cmtEndpoint)
		}),
	)

	if err != nil {
		logger.Error(
			"CometBFT WS client could not be created",
			"address", cmtRPCAddr+cmtEndpoint,
			"error", err,
		)
	} else if err := tmWsClient.OnStart(); err != nil {
		logger.Error(
			"CometBFT WS client could not start",
			"address", cmtRPCAddr+cmtEndpoint,
			"error", err,
		)
	}

	return tmWsClient
}

// MountGRPCWebServices mounts gRPC-Web services on specific HTTP POST routes.
// Parameters:
// - router: The HTTP router instance to mount the routes on (using mux.Router).
// - grpcWeb: The wrapped gRPC-Web server that will handle incoming gRPC-Web and WebSocket requests.
// - grpcResources: A list of resource endpoints (URLs) that should be mounted for gRPC-Web POST requests.
// - logger: A logger instance used to log information about the mounted resources.
func MountGRPCWebServices(
	router *mux.Router,
	grpcWeb *grpcweb.WrappedGrpcServer,
	grpcResources []string,
	logger log.Logger,
) {
	for _, res := range grpcResources {
		logger.Info("[GRPC Web] HTTP POST mounted", "resource", res)

		s := router.Methods("POST").Subrouter()
		s.HandleFunc(res, func(resp http.ResponseWriter, req *http.Request) {
			if grpcWeb.IsGrpcWebSocketRequest(req) {
				grpcWeb.HandleGrpcWebsocketRequest(resp, req)
				return
			}

			if grpcWeb.IsGrpcWebRequest(req) {
				grpcWeb.HandleGrpcWebRequest(resp, req)
				return
			}
		})
	}
}

// Listen starts a net.Listener on the tcp network on the given address.
// If there is a specified MaxOpenConnections in the config, it will also set the limitListener.
func Listen(addr string, config *config.Config) (net.Listener, error) {
	if addr == "" {
		addr = ":http"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if config.JSONRPC.MaxOpenConnections > 0 {
		ln = netutil.LimitListener(ln, config.JSONRPC.MaxOpenConnections)
	}
	return ln, err
}
