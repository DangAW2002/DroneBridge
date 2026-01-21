package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"DroneBridge/auth"
	"DroneBridge/config"
	"DroneBridge/forwarder"
	"DroneBridge/logger"
	"DroneBridge/web"
)

func main() {
	// Parse command-line flags
	configFile := flag.String("config", "config.yaml", "Path to configuration file")
	logLevel := flag.String("log", "", "Log level: debug, info, warn, error (overrides config)")
	register := flag.Bool("register", false, "Register this drone with the fleet server")
	flag.Parse()

	// Load configuration
	logger.Info("Loading configuration from %s", *configFile)
	cfg, err := config.Load(*configFile)
	if err != nil {
		logger.Fatal("Failed to load configuration: %v", err)
	}

	// Set log level from config or command line
	if *logLevel != "" {
		logger.SetLevelFromString(*logLevel)
	} else {
		logger.SetLevelFromString(cfg.Log.Level)
	}

	logger.Info("Configuration loaded successfully (Log level: %s)", logger.GetLevelString())

	// Create single auth client instance - will be reused for both registration and normal operation
	authClient := auth.NewClient(
		cfg.Auth.Host,
		cfg.Auth.Port,
		cfg.Auth.UUID,
		cfg.Auth.SharedSecret,
		cfg.Auth.KeepaliveInterval,
	)

	// Handle registration mode - use the same authClient
	if *register {
		logger.Info("🚀 STARTING REGISTRATION PROCESS")
		logger.Info("Connecting to %s:%d", cfg.Auth.Host, cfg.Auth.Port)

		if err := authClient.Register(); err != nil {
			logger.Fatal("❌ Registration failed: %v", err)
		}

		logger.Info("✅ Registration completed successfully!")
		logger.Info("Secret key has been saved to .drone_secret")
		logger.Info("Now starting normal operation...")
		// Continue to normal operation with same authClient (connection kept alive)
	}

	logger.Info("Listening on port %d, forwarding to %s",
		cfg.Network.LocalListenPort, cfg.GetAddress())

	// Start auth client
	if err := authClient.Start(); err != nil {
		logger.Fatal("Failed to start auth client: %v", err)
	}

	// Wait for auth client to be authenticated (max 10 seconds)
	logger.Info("Waiting for auth client to authenticate with router...")
	for i := 0; i < 100; i++ {
		if authClient.IsAuthenticated() {
			logger.Info("✅ Auth client authenticated with router")
			break
		}
		time.Sleep(100 * time.Millisecond)
		if i == 99 {
			logger.Warn("⚠️ Auth client authentication timeout (10s), continuing anyway")
		}
	}

	// Start web server with auth client and drone UUID
	web.StartServer(cfg.Web.Port, authClient, cfg.Auth.UUID)

	// Create forwarder with shared auth client
	fwd, err := forwarder.New(cfg, authClient)
	if err != nil {
		logger.Fatal("Failed to create forwarder: %v", err)
	}

	// Initialize MAVLink bridge for web server to use (for parameter editing)
	web.InitMAVLinkBridge(fwd.GetListenerNode())

	// Start forwarder
	if err := fwd.Start(); err != nil {
		logger.Fatal("Failed to start forwarder: %v", err)
	}

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	logger.Info("MAVLink forwarder running. Press Ctrl+C to stop.")
	<-sigCh

	// Stop forwarder
	fwd.Stop()
	logger.Info("MAVLink forwarder shutdown complete")
}
