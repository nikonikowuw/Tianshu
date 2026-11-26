package main

import (
	"context"
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
	cfgPath := os.Getenv("TIANSHU_CONFIG")
	paths := []string{"configs/config.example.toml", "configs/config.local.toml"}
	if cfgPath != "" {
		paths = append(paths, cfgPath)
	}

	cfg, err := config.Load(paths...)
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
