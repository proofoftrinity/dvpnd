// SPDX-License-Identifier: Apache-2.0
// Modified from sentinel-official/dvpn-node @ 62bde16 (2024-01-25). See NOTICE.

package cmd

import (
	"bufio"
	gocontext "context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	v1base "github.com/sentinel-official/sentinelhub/v12/types/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/trinitystake/dvpnd/v9/api"
	"github.com/trinitystake/dvpnd/v9/context"
	"github.com/trinitystake/dvpnd/v9/libs/bandwidth"
	"github.com/trinitystake/dvpnd/v9/libs/geoip"
	"github.com/trinitystake/dvpnd/v9/lite"
	"github.com/trinitystake/dvpnd/v9/node"
	"github.com/trinitystake/dvpnd/v9/services"
	"github.com/trinitystake/dvpnd/v9/services/common"
	"github.com/trinitystake/dvpnd/v9/types"
	"github.com/trinitystake/dvpnd/v9/utils"
)

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// bandwidthTimeout bounds the whole bandwidth measurement at start: a few
// transfers of ten to fifteen seconds each plus the server pings, with room to
// spare, but never an open-ended wait on a remote service.
const bandwidthTimeout = 5 * time.Minute

func runHandshake(peers uint64) error {
	return exec.Command("hnsd",
		strings.Split(fmt.Sprintf("--log-file /dev/null "+
			"--pool-size %d "+
			"--rs-host 0.0.0.0:53", peers), " ")...).Run()
}

func StartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the VPN node",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var (
				home         = viper.GetString(flags.FlagHome)
				configPath   = filepath.Join(home, types.ConfigFileName)
				databasePath = filepath.Join(home, types.DatabaseFileName)
			)

			log, err := utils.PrepareLogger()
			if err != nil {
				return err
			}
			common.SetVerbose(utils.DebugLogging())

			if hint := types.LegacyHomeHint(home); hint != "" {
				log.Info(hint)
			}

			v := viper.New()
			v.SetConfigFile(configPath)

			log.Info("Reading the configuration file", "path", configPath)
			config, err := types.ReadInConfig(v)
			if err != nil {
				return err
			}

			skipConfigValidation, err := cmd.Flags().GetBool(flagSkipConfigValidation)
			if err != nil {
				return err
			}

			if !skipConfigValidation {
				log.Info("Validating the configuration", "data", config.Redacted())
				if err = config.Validate(); err != nil {
					return err
				}
			}

			protocol, err := services.Lookup(config.Node.Type)
			if err != nil {
				return err
			}

			service, err := protocol.New(config)
			if err != nil {
				return err
			}

			var (
				input   = bufio.NewReader(cmd.InOrStdin())
				remotes = strings.Split(config.Chain.RPCAddresses, ",")
			)

			log.Info("Initializing the keyring", "name", types.KeyringName, "backend", config.Keyring.Backend)
			kr, err := keyring.New(types.KeyringName, config.Keyring.Backend, home, input, lite.DefaultEncodingConfig().Codec)
			if err != nil {
				return err
			}

			info, err := kr.Key(config.Keyring.From)
			if err != nil {
				return err
			}

			fromAddr, err := info.GetAddress()
			if err != nil {
				return err
			}

			client := lite.NewDefaultClient().
				WithChainID(config.Chain.ID).
				WithFromAddress(fromAddr).
				WithFromName(config.Keyring.From).
				WithGas(config.Chain.Gas).
				WithGasAdjustment(config.Chain.GasAdjustment).
				WithGasPrices(config.Chain.GasPrices).
				WithKeyring(kr).
				WithLogger(log).
				WithQueryTimeout(config.Chain.RPCQueryTimeout).
				WithRemotes(remotes).
				WithSignModeStr("").
				WithSimulateAndExecute(config.Chain.SimulateAndExecute).
				WithTxTimeout(config.Chain.RPCTxTimeout)

			account, err := client.QueryAccount(client.FromAddress())
			if err != nil {
				return err
			}
			if account == nil {
				return fmt.Errorf("account does not exist with address %s", client.FromAddress())
			}

			// The chain deactivates a node, and cancels a session, whose last update
			// is older than the respective status_timeout parameter. Whatever the
			// operator configured, never update less often than 80% of that.
			if params, err := client.QueryNodeParams(); err != nil {
				return err
			} else if limit := params.StatusTimeout * 4 / 5; limit > 0 && config.Node.IntervalUpdateStatus > limit {
				log.Info("Lowering interval_update_status to fit the chain's node status_timeout",
					"configured", config.Node.IntervalUpdateStatus, "status_timeout", params.StatusTimeout, "effective", limit)
				config.Node.IntervalUpdateStatus = limit
			}
			if params, err := client.QuerySessionParams(); err != nil {
				return err
			} else if limit := params.StatusTimeout * 4 / 5; limit > 0 && config.Node.IntervalUpdateSessions > limit {
				log.Info("Lowering interval_update_sessions to fit the chain's session status_timeout",
					"configured", config.Node.IntervalUpdateSessions, "status_timeout", params.StatusTimeout, "effective", limit)
				config.Node.IntervalUpdateSessions = limit
			}

			log.Info("Discovering the public IP and location", "provider", config.GeoIP.Provider)
			location, err := geoip.Location(geoip.Options{
				Provider:  config.GeoIP.Provider,
				URL:       config.GeoIP.URL,
				Token:     config.GeoIP.Token,
				IP:        config.Node.IPv4Address,
				Logger:    log,
				City:      config.GeoIP.City,
				Country:   config.GeoIP.Country,
				Latitude:  config.GeoIP.Latitude,
				Longitude: config.GeoIP.Longitude,
			})
			if err != nil {
				return err
			}
			log.Info("Public IP and location", "ip", location.IP, "city", location.City, "country", location.Country,
				"country_code", location.CountryCode, "source", location.Source)

			// The bandwidth the node advertises: declared in the config, else
			// measured (or read back from the last measurement). Nothing on the
			// chain needs it, so a failure is logged, not fatal: a node that
			// cannot reach a speed-test service still serves clients.
			measureCtx, cancelMeasure := gocontext.WithTimeout(gocontext.Background(), bandwidthTimeout)
			measured := bandwidth.Measure(measureCtx, bandwidth.Options{
				UploadMbps:   config.Bandwidth.UploadMbps,
				DownloadMbps: config.Bandwidth.DownloadMbps,
				Latitude:     location.Latitude,
				Longitude:    location.Longitude,
				IP:           location.IP,
				Home:         home,
				Logger:       log,
			})
			cancelMeasure()
			bw := v1base.NewBandwidthFromInt64(measured.Upload, measured.Download)
			log.Info("Bandwidth to advertise", "upload", bw.Upload, "download", bw.Download, "source", measured.Source)

			if config.Handshake.Enable {
				go func() {
					for {
						log.Info("Starting the Handshake process...")
						if err := runHandshake(config.Handshake.Peers); err != nil {
							log.Error("handshake process exited unexpectedly", "error", err)
						}
					}
				}()
			}

			log.Info("Initializing the VPN service", "type", service.Type())
			if err = service.Init(home); err != nil {
				return err
			}

			log.Info("Starting the VPN service", "type", service.Type())
			if err = service.Start(); err != nil {
				return err
			}

			log.Info("Opening the database", "path", databasePath)
			database, err := gorm.Open(
				// secure_delete zeroes deleted rows, so a session's wallet
				// address and peer key do not linger in free pages of the file.
				sqlite.Open(databasePath+"?_secure_delete=on"),
				&gorm.Config{
					Logger:      logger.Discard,
					PrepareStmt: false,
				},
			)
			if err != nil {
				return err
			}

			log.Info("Migrating the database models...")
			if err = database.AutoMigrate(&types.Session{}); err != nil {
				return err
			}

			var (
				ctx            = context.NewContext()
				router         = gin.New()
				corsMiddleware = cors.New(
					cors.Config{
						AllowAllOrigins: true,
						AllowMethods: []string{
							http.MethodGet,
							http.MethodPost,
						},
						AllowHeaders: []string{
							types.ContentType,
						},
					},
				)
			)

			router.Use(corsMiddleware)
			api.RegisterRoutes(ctx, router)

			ctx = ctx.WithBandwidth(&bw).
				WithBandwidthSource(measured.Source).
				WithClient(client).
				WithConfig(config).
				WithDatabase(database).
				WithHandler(router).
				WithLocation(location).
				WithLogger(log).
				WithService(service)

			n := node.NewNode(ctx)
			if err = n.Initialize(); err != nil {
				return err
			}

			if err = n.ReconcileSessions(); err != nil {
				return err
			}

			// Run until the API server fails or a stop signal arrives, then
			// stop the VPN service so the tunnel interface, NAT rules or the
			// proxy child process do not outlive the node.
			errCh := make(chan error, 1)
			go func() { errCh <- n.Start(home) }()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(sigCh)

			select {
			case sig := <-sigCh:
				log.Info("Stopping: signal received", "signal", sig.String())
			case err = <-errCh:
				log.Error("API server exited", "error", err)
			}

			log.Info("Stopping the VPN service", "type", service.Type())
			if stopErr := service.Stop(); stopErr != nil {
				log.Error("failed to stop the VPN service", "error", stopErr)
				if err == nil {
					err = stopErr
				}
			}

			return err
		},
	}

	cmd.Flags().Bool(flagSkipConfigValidation, false, "skip the validation of configuration")

	return cmd
}
