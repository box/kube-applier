// Package log provides the Logger struct for logging based on go-hclog.
package log

import (
	"sync"

	hclog "github.com/hashicorp/go-hclog"
)

const (
	defaultName = "kube-applier"
)

var (
	loggers = map[string]hclog.Logger{}
	m       = sync.Mutex{}
)

func init() {
	m.Lock()
	defer m.Unlock()
	loggers[defaultName] = hclog.New(&hclog.LoggerOptions{
		Name:            defaultName,
		Level:           hclog.LevelFromString("warn"),
		IncludeLocation: true,
	})
}

// SetLevel sets the global logging level.
func SetLevel(logLevel string) {
	// Since the original logger with defaultName does not set IndependentLevels
	// changing its level will also change the level of sub-loggers.
	loggers[defaultName].SetLevel(hclog.LevelFromString(logLevel))
}

// Logger returns an hclog.Logger with the specified name.
func Logger(name string) hclog.Logger {
	m.Lock()
	defer m.Unlock()
	if _, ok := loggers[name]; !ok {
		loggers[name] = loggers[defaultName].ResetNamed(name)
	}
	return loggers[name]
}
