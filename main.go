package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bluenviron/gomavlib/v3"

	"DroneBridge/config"
	"DroneBridge/internal/auth"
	"DroneBridge/internal/camera"
	"DroneBridge/internal/forwarder"
	"DroneBridge/internal/logger"
	"DroneBridge/web"
)

func main() {
	// Parse command-line flags
	configFile := flag.String("config", "config/config.yaml", "Path to configuration file")
	logLevel := flag.String("log", "", "Log level: debug, info, warn, error (overrides config)")
	register := flag.Bool("register", false, "Register this drone with the fleet server")
	flag.Parse()

	// Create logs directory if it doesn't exist
	if err := os.MkdirAll("logs", 0755); err != nil {
		logger.Warn("Failed to create logs directory: %v", err)
	}

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

	// Set timestamp format from config
	if cfg.Log.TimestampFormat != "" {
		logger.SetTimestampFormat(cfg.Log.TimestampFormat)
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

	// Handle registration mode - SEPARATE from auth
	if *register {
		logger.Info("🚀 STARTING REGISTRATION PROCESS")
		logger.Info("Connecting to %s:%d", cfg.Auth.Host, cfg.Auth.Port)

		if err := authClient.Register(); err != nil {
			logger.Fatal("❌ Registration failed: %v", err)
		}

		logger.Info("✅ Registration completed successfully!")
		logger.Info("Secret key has been saved to .drone_secret")
		logger.Info("Registration connection will be closed, then proceeding with authentication...")

		// IMPORTANT: Registration creates its own TCP connection and closes it automatically
		// We will create a NEW auth connection below (authClient.Start())
		// This ensures registration and auth are completely separate
	}

	logger.Info("Listening on port %d, forwarding to %s",
		cfg.Network.LocalListenPort, cfg.GetAddress())

	// STEP 0: Create listener node ONLY (to listen for Pixhawk, no sender yet)
	logger.Info("[STARTUP] Creating MAVLink listener for Pixhawk...")
	listenerNode, err := forwarder.NewListener(cfg)
	if err != nil {
		logger.Fatal("Failed to create listener: %v", err)
	}

	// Initialize MAVLink bridge EARLY with listener node (for web access)
	web.InitMAVLinkBridge(listenerNode)

	// STEP 1: Wait for Pixhawk connection by starting a minimal forwarder loop
	// This just listens and captures System ID from heartbeat
	logger.Info("[STARTUP] ⏳ Waiting for Pixhawk heartbeat... (timeout: %ds)", cfg.Ethernet.PixhawkConnectionTimeout)
	pixhawkSysID := uint8(0)
	pixhawkConnected := false
	pixhawkReadyCh := make(chan struct{}) // Channel to signal when done (connected or timeout)

	// Start listening on the listener node
	go func() {
		eventCh := listenerNode.Events()
		timeout := time.NewTimer(time.Duration(cfg.Ethernet.PixhawkConnectionTimeout) * time.Second)
		defer timeout.Stop()

		for {
			select {
			case <-timeout.C:
				pixhawkReadyCh <- struct{}{} // Signal timeout
				return
			case event := <-eventCh:
				if frame, ok := event.(*gomavlib.EventFrame); ok {
					pixhawkSysID = frame.SystemID()
					pixhawkConnected = true
					logger.Info("[PIXHAWK_CONNECTED] ✅ First heartbeat received from Pixhawk (SysID: %d)", pixhawkSysID)
					web.HandleHeartbeat(pixhawkSysID)
					pixhawkReadyCh <- struct{}{} // Signal connected immediately
					return
				}
			}
		}
	}()

	// Wait for signal (either connected or timeout)
	<-pixhawkReadyCh

	if pixhawkConnected {
		logger.Info("[STARTUP] ✅ Pixhawk connected successfully!")
		logger.Info("[STARTUP] Pixhawk System ID: %d", pixhawkSysID)
	} else {
		if cfg.Ethernet.AllowMissingPixhawk {
			logger.Warn("[STARTUP] ⚠️  Pixhawk connection timeout, but AllowMissingPixhawk=true, continuing...")
			logger.Warn("[STARTUP] ⚠️  Running in DEBUG mode without actual Pixhawk connection!")
		} else {
			logger.Fatal("[STARTUP] ❌ Pixhawk connection failed. Set 'allow_missing_pixhawk: true' in config to skip this requirement.")
		}
	}

	// STEP 2: Now create full forwarder (with sender node using correct SysID)
	logger.Info("[STARTUP] ✈️  Creating forwarder with correct System ID...")
	fwd, err := forwarder.New(cfg, nil, listenerNode) // Pass listenerNode to reuse it
	if err != nil {
		logger.Fatal("Failed to create forwarder: %v", err)
	}

	// STEP 3: Start forwarder
	logger.Info("[STARTUP] Starting forwarder...")
	if err := fwd.Start(); err != nil {
		logger.Fatal("Failed to start forwarder: %v", err)
	}

	// STEP 4: Authenticate with server
	logger.Info("[STARTUP] ✈️  Now proceeding with server authentication...")

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

	// STEP 5: Initialize video streaming
	logger.Info("[STARTUP] 📹 Initializing video streaming...")

	if cfg.Camera.Enabled {
		// Convert YAML config to streaming config
		streamingCfg := &camera.StreamingConfig{
			CameraID:         cfg.Camera.CameraID,
			Size:             []int{cfg.Camera.Resolution.Width, cfg.Camera.Resolution.Height},
			Framerate:        cfg.Camera.Framerate,
			Format:           cfg.Camera.Format,
			MediaMTXHost:     cfg.Camera.MediaMTX.Host,
			MediaMTXPort:     cfg.Camera.MediaMTX.Port,
			DroneID:          cfg.Auth.UUID, // Use auth UUID automatically
			Bitrate:          cfg.Camera.Encoder.Bitrate,
			OverlayEnabled:   cfg.Camera.Features.Overlay,
			DetectionEnabled: cfg.Camera.Features.Detection,
			KeyframeInterval: cfg.Camera.Encoder.KeyframeInterval,
			Preset:           cfg.Camera.Encoder.Preset,
			Tune:             cfg.Camera.Encoder.Tune,
			Enabled:          cfg.Camera.Enabled,
		}

		if err := camera.InitializeFromConfig(streamingCfg, cfg.Auth.Host, cfg.Auth.UUID); err != nil {
			logger.Warn("[STARTUP] Failed to initialize camera: %v", err)
		} else {
			if err := camera.StartAllCameras(); err != nil {
				logger.Warn("[STARTUP] Failed to start cameras: %v", err)
			} else {
				logger.Info("[STARTUP] ✅ Video streaming initialized")
			}
		}
	} else {
		logger.Info("[STARTUP] Video streaming disabled in config")
	}

	// Start web server with auth client and drone UUID
	web.StartServer(cfg.Web.Port, authClient, cfg.Auth.UUID)

	// Now set auth client on forwarder and re-wire callbacks
	fwd.SetAuthClient(authClient)

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	logger.Info("MAVLink forwarder running. Press Ctrl+C to stop.")
	<-sigCh

	// Graceful shutdown
	logger.Info("[SHUTDOWN] Initiating graceful shutdown...")

	// Stop cameras first
	camera.GracefulShutdown()

	// Stop forwarder
	fwd.Stop()

	// Cleanup resources
	camera.Cleanup()

	logger.Info("[SHUTDOWN] ✅ Complete")
}
