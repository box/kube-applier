package log

import (
	hclog "github.com/hashicorp/go-hclog"
)

var Logger hclog.Logger

func InitLogger(logLevel string) {
	Logger = hclog.New(&hclog.LoggerOptions{
		Name:  "kube-applier",
		Level: hclog.LevelFromString(logLevel),
	})
}
