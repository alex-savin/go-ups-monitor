// Command go-ups starts the UPS monitoring server.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alex-savin/go-ups-monitor/config"
	"github.com/alex-savin/go-ups-monitor/monitor"
	"github.com/alex-savin/go-ups-monitor/server"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	logger := config.NewLogger(cfg.Logging)
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("Received shutdown signal")
		cancel()
	}()

	// Create monitor
	monCfg := monitor.DefaultConfig()
	monCfg.Logger = logger
	mon := monitor.New(monCfg)

	// Start device pollers
	for _, device := range cfg.Devices {
		mon.StartDevice(ctx, device)
	}
	defer mon.StopAll()

	// Create and start server
	srvCfg := server.DefaultConfig()
	srvCfg.Host = cfg.Server.Host
	srvCfg.Port = cfg.Server.Port
	srvCfg.Logger = logger

	configPath := os.Getenv("UPS_CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yml"
	}

	srv := server.New(srvCfg, cfg, configPath, mon)
	if err := srv.Start(ctx); err != nil {
		logger.Error("Server error", "error", err)
		os.Exit(1)
	}
}
