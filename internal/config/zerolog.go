package config

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func ConfigureZeroLog(logLevelStr string) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// configure global logger
	if logLevelStr == "" {
		logLevelStr = "info" // Default log level
	}
	if logLevelStr == "debug" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	}

	// Parse log level
	logLevel, err := zerolog.ParseLevel(strings.ToLower(logLevelStr))
	if err != nil {
		log.Error().Err(err).Msg("Invalid log level, using default (info)")
		logLevel = zerolog.InfoLevel
	}
	// Set global log level
	zerolog.SetGlobalLevel(logLevel)
}
