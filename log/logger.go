// Package log provides the Logger struct for logging based on go-hclog.
package log

import (
	"sync"

	hclog "github.com/hashicorp/go-hclog"
)

const (
	defaultLogLevel = "warn"
)

var (
	level   = hclog.LevelFromString(defaultLogLevel)
	loggers = map[string]hclog.Logger{}
	m       = sync.Mutex{}
)

// SetLevel sets the global logging level
func SetLevel(logLevel string) {
	level = hclog.LevelFromString(logLevel)
	for _, l := range loggers {
		l.SetLevel(level)
	}
}

// Logger returns an hclog.Logger with the specified name
func Logger(name string) hclog.Logger {
	m.Lock()
	defer m.Unlock()
	if _, ok := loggers[name]; ok {
		return loggers[name]
	}
	loggers[name] = hclog.New(&hclog.LoggerOptions{
		Name:  name,
		Level: level,
	})
	return loggers[name]
}
