package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"arcaeabot/internal/config"
	"arcaeabot/internal/database"
	"arcaeabot/internal/debundler"
	"arcaeabot/internal/logging"
	"arcaeabot/internal/napcat"
	_ "arcaeabot/internal/plugins"
	"arcaeabot/internal/utils"
)

func main() {
	logging.Setup()
	configPath := flag.String("config", "config.yaml", "path to the YAML configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.OpenStore(filepath.Join(cfg.DataPath, "arcaeabot.db"))
	if err != nil {
		slog.Error("open plugin database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	arcDB, err := database.Open(database.Config{
		URL: cfg.ArcaeaDatabaseURL,
	})
	if err != nil {
		slog.Error("connect arcaea database", "error", err)
		os.Exit(1)
	}
	defer arcDB.Close()

	arcLogDB, err := database.Open(database.Config{
		URL: cfg.ArcaeaLogDatabaseURL,
	})
	if err != nil {
		slog.Error("connect arcaea log database", "error", err)
		os.Exit(1)
	}
	defer arcLogDB.Close()

	if err := debundler.Run(cfg); err != nil {
		slog.Error("run debundler", "bundle", cfg.DebundlerBundlePath, "output", cfg.DebundlerOutputPath, "error", err)
		os.Exit(1)
	}
	slog.Info("debundler completed", "bundle", cfg.DebundlerBundlePath, "output", cfg.DebundlerOutputPath)

	client, err := napcat.Connect(ctx, cfg.WSURL, cfg.WSToken)
	if err != nil {
		slog.Error("connect napcat", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	if info, err := client.Call(ctx, "get_login_info", nil); err == nil {
		client.SetSelfID(info.Int("user_id"))
	}
	slog.Info("ArcaeaBot is running", "qq", client.SelfID())

	registry := utils.NewRegistry(client, db, arcDB, arcLogDB, cfg)
	registry.Load()
	slog.Info("plugins loaded", "count", registry.Count())

	sem := make(chan struct{}, cfg.ThreadCount)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-client.Events():
			if !ok {
				return
			}
			sem <- struct{}{}
			go func(ev napcat.Event) {
				defer func() { <-sem }()
				if err := registry.Handle(ctx, ev); err != nil {
					slog.Error("handle event", "error", err)
				}
			}(event)
		}
	}
}
