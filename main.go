package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
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

	// Debug overrides
	overrideListenPort := flag.Int("listen-port", 0, "Override local UDP listen port")
	overrideWebPort := flag.Int("web-port", 0, "Override web server port")
	overrideUUID := flag.String("uuid", "", "Override Drone UUID")
	overrideServer := flag.String("server", "", "Override Server Host")
	overrideServerPort := flag.Int("server-port", 0, "Override Server Port")
	overrideBroadcastPort := flag.Int("broadcast-port", -1, "Override UDP broadcast bind port (0=random, -1=disabled/auto)")

	// Test Mode
	testMode := flag.Bool("test-mode", false, "Enable test mode (uses test_mode/ folder for secrets)")

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

	// Apply Command Line Overrides
	if *overrideListenPort > 0 {
		logger.Info("🔧 [OVERRIDE] Local Listen Port: %d -> %d", cfg.Network.LocalListenPort, *overrideListenPort)
		cfg.Network.LocalListenPort = *overrideListenPort
	}
	if *overrideWebPort > 0 {
		logger.Info("🔧 [OVERRIDE] Web Port: %d -> %d", cfg.Web.Port, *overrideWebPort)
		cfg.Web.Port = *overrideWebPort
	}
	if *overrideUUID != "" {
		logger.Info("🔧 [OVERRIDE] Drone UUID: %s -> %s", cfg.Auth.UUID, *overrideUUID)
		cfg.Auth.UUID = *overrideUUID
	}

	// TEST MODE LOGIC
	if *testMode {
		logger.Info("🧪 [TEST MODE] ACTIVATED")

		// Ensure test_mode directory exists
		testDir := "test_mode"
		if err := os.MkdirAll(testDir, 0755); err != nil {
			logger.Warn("Failed to create test_mode directory: %v", err)
		}

		// Use secret file in test_mode folder
		// e.g. test_mode/.drone_secret_<uuid>
		customSecretFile := filepath.Join(testDir, fmt.Sprintf(".drone_secret_%s", cfg.Auth.UUID))
		auth.SetSecretFileName(customSecretFile)
		logger.Info("🧪 [TEST MODE] Using isolated secret file: %s", customSecretFile)
	}
	if *overrideServer != "" {
		logger.Info("🔧 [OVERRIDE] Auth Host: %s -> %s", cfg.Auth.Host, *overrideServer)
		cfg.Auth.Host = *overrideServer
	}
	if *overrideServerPort > 0 {
		logger.Info("🔧 [OVERRIDE] Auth Port: %d -> %d", cfg.Auth.Port, *overrideServerPort)
		cfg.Auth.Port = *overrideServerPort
	}
	if *overrideBroadcastPort >= 0 {
		logger.Info("🔧 [OVERRIDE] Broadcast Port: %d -> %d", cfg.Network.BroadcastPort, *overrideBroadcastPort)
		cfg.Network.BroadcastPort = *overrideBroadcastPort
	}

	// Set log level from config or command line
	if *logLevel != "" {
		logger.Info("🔧 [OVERRIDE] Log Level: %s -> %s", cfg.Log.Level, *logLevel)
		logger.SetLevelFromString(*logLevel)
	} else {
		logger.SetLevelFromString(cfg.Log.Level)
	}

	// Set timestamp format from config
	if cfg.Log.TimestampFormat != "" {
		logger.SetTimestampFormat(cfg.Log.TimestampFormat)
	}

	// VALIDATE UUID FORMAT
	if !isValidUUID(cfg.Auth.UUID) {
		logger.Fatal("❌ Invalid Drone UUID format: '%s'. strictly UUID (8-4-4-4-12 hex) required.", cfg.Auth.UUID)
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

	// STEP 0: Discover Pixhawk (Transient Phase)
	logger.Info("[STARTUP] ⏳ Entering Discovery Phase...")
	discoveredIP, discoveredSysID, discErr := forwarder.DiscoverPixhawk(cfg, time.Duration(cfg.Ethernet.PixhawkConnectionTimeout)*time.Second)

	var listenerNode *gomavlib.Node
	if discErr == nil {
		logger.Info("[STARTUP] ✅ Pixhawk discovered at %s (System ID: %d)", discoveredIP, discoveredSysID)
		// Register found SysID with web bridge early
		web.HandleHeartbeat(discoveredSysID)

		// Create CLEAN Unicast listener
		listenerNode, err = forwarder.NewListener(cfg, discoveredIP)
	} else {
		if cfg.Ethernet.AllowMissingPixhawk {
			logger.Warn("[STARTUP] ⚠️  Discovery failed (%v), but AllowMissingPixhawk=true, continuing with Broadcast fallback...", discErr)
			listenerNode, err = forwarder.NewListener(cfg, "")
		} else {
			logger.Fatal("[STARTUP] ❌ Pixhawk discovery failed: %v. Set 'allow_missing_pixhawk: true' to skip.", discErr)
		}
	}

	if err != nil {
		logger.Fatal("Failed to create listener: %v", err)
	}

	// Initialize MAVLink bridge EARLY with listener node (for web access)
	web.InitMAVLinkBridge(listenerNode)

	// Since we either discovered it or we are in fallback, we proceed.
	// If it was discovered, the listenerNode is already connected via Unicast.
	// If discovery failed but we allowed it, we are using Broadcast fallback.
	pixhawkConnected := (discErr == nil)
	pixhawkSysID := discoveredSysID

	if !pixhawkConnected && cfg.Ethernet.AllowMissingPixhawk {
		// We need to wait for a heartbeat if we fell back to broadcast
		logger.Info("[STARTUP] ⏳ Waiting for Pixhawk heartbeat via Broadcast fallback...")
		pixhawkReadyCh := make(chan struct{})

		go func() {
			eventCh := listenerNode.Events()
			timeout := time.NewTimer(10 * time.Second) // Small additional timeout
			defer timeout.Stop()

			for {
				select {
				case <-timeout.C:
					pixhawkReadyCh <- struct{}{}
					return
				case event := <-eventCh:
					if frame, ok := event.(*gomavlib.EventFrame); ok {
						pixhawkSysID = frame.SystemID()
						pixhawkConnected = true
						logger.Info("[PIXHAWK_CONNECTED] ✅ Received heartbeat via fallback (SysID: %d)", pixhawkSysID)
						web.HandleHeartbeat(pixhawkSysID)
						pixhawkReadyCh <- struct{}{}
						return
					}
				}
			}
		}()
		<-pixhawkReadyCh
	}

	// STEP 2: Now create full forwarder (with sender node using correct SysID)
	logger.Info("[STARTUP] ✈️  Creating forwarder with correct System ID...")
	fwd, err := forwarder.New(cfg, nil, listenerNode, pixhawkSysID) // Pass listenerNode and discovered SysID
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

// isValidUUID checks if the string is a valid UUID
func isValidUUID(u string) bool {
	r := regexp.MustCompile("^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$")
	return r.MatchString(u)
}
