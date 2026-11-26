package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"tianshu/internal/app"
	"tianshu/internal/platform/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "tianshu: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var configFlag string
	flag.StringVar(&configFlag, "config", "", "path to TOML config file (overrides default config.toml search)")
	flag.Parse()

	explicitPath := configFlag
	if explicitPath == "" {
		explicitPath = os.Getenv("TIANSHU_CONFIG")
	}

	cfg, err := config.LoadWithExplicit(explicitPath, config.DefaultPaths)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, cfg, app.Options{})
	if err != nil {
		return err
	}
	return application.Run(ctx)
}
