package main

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/charmbracelet/crush/internal/cmd"
	_ "github.com/joho/godotenv/autoload"
	"github.com/kawai-network/y/paths"
)

func main() {
	// Use local data directory in development, user path in production.
	if os.Getenv("VERIDIUM_DEV") == "1" {
		paths.SetDataDir("data")
	} else {
		paths.SetDataDir(paths.UserDataDir())
	}

	if os.Getenv("CRUSH_PROFILE") != "" {
		go func() {
			slog.Info("Serving pprof at localhost:6060")
			if httpErr := http.ListenAndServe("localhost:6060", nil); httpErr != nil {
				slog.Error("Failed to pprof listen", "error", httpErr)
			}
		}()
	}

	cmd.Execute()
}
