// Package log provides the Logger struct for logging based on go-hclog.
package log

import (
	hclog "github.com/hashicorp/go-hclog"
)

const (
	defaultLogLevel = "warn"
)

var (
	level   = hclog.LevelFromString(defaultLogLevel)
	loggers = map[string]hclog.Logger{}
)

// SetLevel sets the global logging level
func SetLevel(logLevel string) {
	level = hclog.LevelFromString(logLevel)
}

// Logger returns an hclog.Logger with the specified name
func Logger(name string) hclog.Logger {
	if l, ok := loggers[name]; ok {
		return l
	}
	loggers[name] = hclog.New(&hclog.LoggerOptions{
		Name:  name,
		Level: level,
	})
	return loggers[name]
}
