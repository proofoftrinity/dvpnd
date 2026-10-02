// SPDX-License-Identifier: Apache-2.0
// Modified from sentinel-official/dvpn-node @ 62bde16 (2024-01-25). See NOTICE.

package utils

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cometbft/cometbft/config"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

// zeroLogger adapts zerolog to the CometBFT logger interface. cosmos-sdk v0.47
// no longer ships server.ZeroLogWrapper, which upstream used here.
type zeroLogger struct {
	zerolog.Logger
}

func (l zeroLogger) Debug(msg string, keyVals ...interface{}) {
	l.Logger.Debug().Fields(fields(keyVals)).Msg(msg)
}

func (l zeroLogger) Info(msg string, keyVals ...interface{}) {
	l.Logger.Info().Fields(fields(keyVals)).Msg(msg)
}

func (l zeroLogger) Error(msg string, keyVals ...interface{}) {
	l.Logger.Error().Fields(fields(keyVals)).Msg(msg)
}

func (l zeroLogger) With(keyVals ...interface{}) cmtlog.Logger {
	return zeroLogger{l.Logger.With().Fields(fields(keyVals)).Logger()}
}

// fields turns the CometBFT-style alternating key/value list into a map,
// stringifying values zerolog cannot serialise directly.
func fields(keyVals []interface{}) map[string]interface{} {
	if len(keyVals)%2 != 0 {
		keyVals = append(keyVals, "(missing)")
	}

	m := make(map[string]interface{}, len(keyVals)/2)
	for i := 0; i < len(keyVals); i += 2 {
		key := fmt.Sprint(keyVals[i])
		switch v := keyVals[i+1].(type) {
		case fmt.Stringer:
			m[key] = v.String()
		case error:
			m[key] = v.Error()
		default:
			m[key] = v
		}
	}

	return m
}

func PrepareLogger() (cmtlog.Logger, error) {
	var (
		format           = viper.GetString(flags.FlagLogFormat)
		level            = viper.GetString(flags.FlagLogLevel)
		writer io.Writer = os.Stderr
	)

	if format == config.LogFormatPlain {
		writer = zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		}
	}

	logLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		return nil, err
	}

	return zeroLogger{
		zerolog.New(writer).
			Level(logLevel).
			With().Timestamp().
			Logger(),
	}, nil
}

// DebugLogging reports whether the node logs at debug level or finer, the
// level at which an operator has asked for per-connection detail.
func DebugLogging() bool {
	level, err := zerolog.ParseLevel(viper.GetString(flags.FlagLogLevel))

	return err == nil && level <= zerolog.DebugLevel
}
