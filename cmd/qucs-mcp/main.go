package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/lau/qucs-mcp/internal/config"
	"github.com/lau/qucs-mcp/internal/server"
)

func main() {
	cfg := config.Load()
	srv := server.NewServer(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if !quiet() {
		fmt.Fprintf(os.Stderr, "%s %s ready (qucs-s: %s)\n", cfg.ServerName, cfg.ServerVersion, cfg.QucsExe())
	}
	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func quiet() bool {
	for _, a := range os.Args[1:] {
		if a == "-q" || a == "--quiet" {
			return true
		}
	}
	return false
}
