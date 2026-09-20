package main

import (
	"log/slog"

	"github.com/Sake-My/Komari-Nova/cmd"
	"github.com/Sake-My/Komari-Nova/utils"
	logger "github.com/Sake-My/Komari-Nova/utils/log"
)

func main() {
	if utils.VersionHash == "unknown" {
		logger.Setup(slog.LevelDebug)
	} else {
		logger.Setup(slog.LevelInfo)
	}

	logger.Infof("server", "Komari Monitor %s (hash: %s)", utils.CurrentVersion, utils.VersionHash)

	cmd.Execute()
}
