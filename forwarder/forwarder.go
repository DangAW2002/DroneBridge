package forwarder

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"

	"DroneBridge/auth"
	"DroneBridge/config"
	"DroneBridge/logger"
	"DroneBridge/mavlink_custom"
	"DroneBridge/metrics"
	"DroneBridge/web"
)

// getMessageTypeName extracts clean message type name from message
// e.g., *common.MessageHeartbeat -> HEARTBEAT
func getMessageTypeName(msg interface{}) string {
	fullType := fmt.Sprintf("%T", msg)

	// Remove *common. prefix if exists
	if strings.HasPrefix(fullType, "*common.Message") {
		name := strings.TrimPrefix(fullType, "*common.Message")
		return name
	}
	// Remove common. prefix if exists
	if strings.HasPrefix(fullType, "common.Message") {
		name := strings.TrimPrefix(fullType, "common.Message")
		return name
	}
	// Remove Message prefix if exists
	if strings.HasPrefix(fullType, "Message") {
		name := strings.TrimPrefix(fullType, "Message")
		return name
	}
	return fullType
}

// Forwarder handles receiving real MAVLink messages from Pixhawk and forwarding to server
type Forwarder struct {
	cfg          *config.Config
	listenerNode *gomavlib.Node // Listens for messages from Pixhawk and sends heartbeats
	senderNode   *gomavlib.Node // Sends messages to server
	authClient   *auth.Client
	stopCh       chan struct{}
	previousIP   string // Track previous local IP for change detection

	// Network health
	isHealthy    bool
	forceCheckCh chan struct{}
	mu           sync.RWMutex

	// Logging control
	lastHeartbeatLog time.Time
	lastGPSLog       time.Time
	lastAttitudeLog  time.Time

	// UDP heartbeat status
	udpHeartbeatSent chan struct{} // Signal when first UDP heartbeat sent

	// Deduplication - track seen messages by sequence number
	lastSeqNum map[uint8]uint8 // SystemID -> last sequence number
	seqMu      sync.RWMutex
}

// getLocalIP returns the current local IP address used for outbound connections
func getLocalIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// getEthernetIP automatically detects the IP address of an ethernet interface
// It searches for interfaces matching common ethernet naming patterns: eth*, end*, enp*, eno*
// Returns the IP address and broadcast address for the found interface
func getEthernetIP(cfg *config.Config) (localIP string, broadcastIP string, ifaceName string, err error) {
	// If local IP is configured, check if it exists on an interface
	if cfg.Ethernet.LocalIP != "" {
		localIP = cfg.Ethernet.LocalIP

		// Calculate broadcast if not provided
		if cfg.Ethernet.BroadcastIP != "" {
			broadcastIP = cfg.Ethernet.BroadcastIP
		} else {
			// Calculate from local IP assuming /24 subnet
			ipParts := strings.Split(localIP, ".")
			if len(ipParts) == 4 {
				broadcastIP = fmt.Sprintf("%s.%s.%s.255", ipParts[0], ipParts[1], ipParts[2])
			}
		}

		// Check if the configured IP exists on any interface
		ifaces, err := net.Interfaces()
		if err != nil {
			return "", "", "", fmt.Errorf("failed to get network interfaces: %w", err)
		}

		ipExists := false
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}

			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}

			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}

				if ip != nil && ip.To4() != nil && ip.String() == localIP {
					ipExists = true
					ifaceName = iface.Name
					break
				}
			}
			if ipExists {
				break
			}
		}

		if ipExists {
			logger.Info("[NETWORK] Using configured ethernet: IP=%s, Broadcast=%s", localIP, broadcastIP)
			return localIP, broadcastIP, ifaceName, nil
		} else if cfg.Ethernet.AutoSetup {
			// IP not found, try to auto-setup on detected interface
			ethPatterns := []string{"eth", "end", "enp", "eno"}
			if cfg.Ethernet.Interface != "" {
				ethPatterns = []string{cfg.Ethernet.Interface}
			}

			for _, iface := range ifaces {
				if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
					continue
				}

				isMatch := false
				for _, pattern := range ethPatterns {
					if cfg.Ethernet.Interface != "" {
						if iface.Name == pattern {
							isMatch = true
							break
						}
					} else {
						if strings.HasPrefix(iface.Name, pattern) {
							isMatch = true
							break
						}
					}
				}

				if isMatch {
					logger.Info("[NETWORK] Configured IP %s not found, attempting to auto-setup on %s...", localIP, iface.Name)
					if err := setupInterfaceIP(iface.Name, cfg.Ethernet.LocalIP, cfg.Ethernet.Subnet); err != nil {
						logger.Warn("[NETWORK] Failed to auto-setup IP on %s: %v", iface.Name, err)
						continue
					} else {
						ifaceName = iface.Name
						logger.Info("[NETWORK] Auto-configured %s with IP=%s", ifaceName, localIP)
						return localIP, broadcastIP, ifaceName, nil
					}
				}
			}
			return "", "", "", fmt.Errorf("configured IP %s not found and auto-setup failed", localIP)
		} else {
			return "", "", "", fmt.Errorf("configured IP %s not found on any interface", localIP)
		}
	}

	// Auto-detect from interface
	ethPatterns := []string{"eth", "end", "enp", "eno"}

	// If specific interface is configured, only look for that
	if cfg.Ethernet.Interface != "" {
		ethPatterns = []string{cfg.Ethernet.Interface}
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get network interfaces: %w", err)
	}

	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Check if interface name matches patterns
		isMatch := false
		for _, pattern := range ethPatterns {
			if cfg.Ethernet.Interface != "" {
				// Exact match if interface is specified
				if iface.Name == pattern {
					isMatch = true
					break
				}
			} else {
				// Prefix match for auto-detect
				if strings.HasPrefix(iface.Name, pattern) {
					isMatch = true
					break
				}
			}
		}

		if !isMatch {
			continue
		}

		ifaceName = iface.Name

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			var ipNet *net.IPNet

			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
				ipNet = v
			case *net.IPAddr:
				ip = v.IP
			}

			// Skip IPv6 addresses
			if ip == nil || ip.To4() == nil {
				continue
			}

			localIP = ip.String()

			// Calculate broadcast address
			if ipNet != nil {
				broadcast := make(net.IP, len(ip.To4()))
				for i := range ip.To4() {
					broadcast[i] = ip.To4()[i] | ^ipNet.Mask[i]
				}
				broadcastIP = broadcast.String()
			} else {
				ipParts := strings.Split(localIP, ".")
				if len(ipParts) == 4 {
					broadcastIP = fmt.Sprintf("%s.%s.%s.255", ipParts[0], ipParts[1], ipParts[2])
				}
			}

			logger.Info("[NETWORK] Auto-detected ethernet interface %s: IP=%s, Broadcast=%s", iface.Name, localIP, broadcastIP)
			return localIP, broadcastIP, ifaceName, nil
		}

		// Interface found but no IP - try to configure if auto_setup is enabled
		if cfg.Ethernet.AutoSetup && cfg.Ethernet.LocalIP != "" {
			logger.Info("[NETWORK] Interface %s has no IP, attempting to configure...", iface.Name)
			if err := setupInterfaceIP(iface.Name, cfg.Ethernet.LocalIP, cfg.Ethernet.Subnet); err != nil {
				logger.Warn("[NETWORK] Failed to auto-setup IP: %v", err)
			} else {
				localIP = cfg.Ethernet.LocalIP
				ipParts := strings.Split(localIP, ".")
				if len(ipParts) == 4 {
					broadcastIP = fmt.Sprintf("%s.%s.%s.255", ipParts[0], ipParts[1], ipParts[2])
				}
				logger.Info("[NETWORK] Auto-configured %s with IP=%s", iface.Name, localIP)
				return localIP, broadcastIP, iface.Name, nil
			}
		}
	}

	return "", "", "", fmt.Errorf("no ethernet interface found (patterns: %v)", ethPatterns)
}

// setupInterfaceIP configures an IP address on an interface using ip command
func setupInterfaceIP(ifaceName, ipAddr, subnet string) error {
	if subnet == "" {
		subnet = "24"
	}
	cmd := exec.Command("sudo", "ip", "addr", "add", fmt.Sprintf("%s/%s", ipAddr, subnet), "dev", ifaceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if IP already exists
		if strings.Contains(string(output), "File exists") {
			logger.Debug("[NETWORK] IP %s already exists on %s", ipAddr, ifaceName)
			return nil
		}
		return fmt.Errorf("failed to add IP: %s - %v", string(output), err)
	}
	return nil
}

// New creates a new forwarder instance
func New(cfg *config.Config, authClient *auth.Client) (*Forwarder, error) {
	// Use provided auth client (already created and authenticated in main.go)
	// This ensures both web server and forwarder use the SAME session token
	if cfg.Auth.Enabled && authClient == nil {
		logger.Warn("Authentication enabled but no authClient provided - creating new one")
		authClient = auth.NewClient(
			cfg.Auth.Host,
			cfg.Auth.Port,
			cfg.Auth.UUID,
			cfg.Auth.SharedSecret,
			cfg.Auth.KeepaliveInterval,
		)
	} else if cfg.Auth.Enabled {
		logger.Info("Authentication enabled, using shared authClient for drone UUID %s",
			cfg.Auth.UUID)
	} else {
		logger.Warn("Authentication disabled - running in insecure mode")
	}

	// Get ethernet IP for UDP broadcast
	localEthIP, broadcastEthIP, ifaceName, ethErr := getEthernetIP(cfg)

	// Build endpoints list
	endpoints := []gomavlib.EndpointConf{
		gomavlib.EndpointUDPServer{Address: fmt.Sprintf("0.0.0.0:%d", cfg.Network.LocalListenPort)},
	}

	// Only add UDP broadcast endpoint if ethernet interface was found
	if ethErr == nil && localEthIP != "" && broadcastEthIP != "" {
		endpoints = append(endpoints, gomavlib.EndpointUDPBroadcast{
			BroadcastAddress: fmt.Sprintf("%s:%d", broadcastEthIP, cfg.Network.LocalListenPort),
			LocalAddress:     fmt.Sprintf("%s:%d", localEthIP, cfg.Network.LocalListenPort+1),
		})
		logger.Info("[NETWORK] UDP Broadcast enabled on %s: Local=%s:%d, Broadcast=%s:%d",
			ifaceName, localEthIP, cfg.Network.LocalListenPort+1, broadcastEthIP, cfg.Network.LocalListenPort)
	} else {
		logger.Warn("[NETWORK] UDP Broadcast disabled: %v", ethErr)
		logger.Info("[NETWORK] Running with UDP Server only on 0.0.0.0:%d", cfg.Network.LocalListenPort)
	}

	// Create listener node to receive from Pixhawk
	listenerNode, err := gomavlib.NewNode(gomavlib.NodeConf{
		Endpoints:   endpoints,
		Dialect:     mavlink_custom.GetCombinedDialect(),
		OutVersion:  gomavlib.V2,
		OutSystemID: 255, // Ground station ID
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create listener MAVLink node: %w", err)
	}
	logger.Info("MAVLink listener created on port %d", cfg.Network.LocalListenPort)

	// Create sender node to forward to server
	senderNode, err := gomavlib.NewNode(gomavlib.NodeConf{
		Endpoints: []gomavlib.EndpointConf{
			gomavlib.EndpointUDPClient{Address: cfg.GetAddress()},
		},
		Dialect:     mavlink_custom.GetCombinedDialect(),
		OutVersion:  gomavlib.V2,
		OutSystemID: 1, // Not actually used since we forward raw frames
	})
	if err != nil {
		listenerNode.Close()
		return nil, fmt.Errorf("failed to create sender MAVLink node: %w", err)
	}
	logger.Info("MAVLink sender created, forwarding to %s", cfg.GetAddress())

	// Get initial local IP
	localIP, err := getLocalIP()
	if err != nil {
		logger.Warn("Failed to get local IP: %v", err)
		localIP = ""
	}

	fwd := &Forwarder{
		cfg:              cfg,
		listenerNode:     listenerNode,
		senderNode:       senderNode,
		authClient:       authClient,
		stopCh:           make(chan struct{}),
		previousIP:       localIP,
		isHealthy:        true,
		forceCheckCh:     make(chan struct{}, 1),
		udpHeartbeatSent: make(chan struct{}, 1),
		lastSeqNum:       make(map[uint8]uint8),
	}

	// Wire up network error callback
	if authClient != nil {
		authClient.OnNetworkError = func() {
			fwd.mu.Lock()
			if fwd.isHealthy {
				logger.Warn("[NETWORK] Network error detected via Auth Client - Marking unhealthy")
				fwd.isHealthy = false
				// Trigger immediate IP check
				select {
				case fwd.forceCheckCh <- struct{}{}:
				default:
				}
			}
			fwd.mu.Unlock()
		}
	}

	return fwd, nil
}

// GetListenerNode returns the listener MAVLink node for external use
func (f *Forwarder) GetListenerNode() *gomavlib.Node {
	return f.listenerNode
}

// Start begins the forwarder
func (f *Forwarder) Start() error {
	logger.Info("Starting MAVLink forwarder...")

	// Authenticate first if enabled
	if f.authClient != nil {
		logger.Info("Authenticating with server...")
		if err := f.authClient.Start(); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		logger.Info("Authentication successful - ready to forward MAVLink packets!")
	}

	// Start IP change monitor
	go f.monitorIPChange()

	// Wait for first UDP heartbeat before starting to forward
	// (Server needs to know we exist before accepting our MAVLink stream)
	if f.authClient != nil {
		logger.Info("Waiting for first UDP heartbeat to be sent...")
		select {
		case <-f.udpHeartbeatSent:
			logger.Info("First UDP heartbeat sent - now starting MAVLink forwarding")
		case <-time.After(5 * time.Second):
			logger.Warn("Timeout waiting for UDP heartbeat, starting anyway...")
		}
	}

	// Start receiving and forwarding messages
	go f.receiveAndForward()
	go f.receiveFromServer()
	go f.sendHeartbeat()
	go f.sendMavlinkSessionHeartbeat() // MAVLink-wrapped session heartbeat for IP:Port sync

	logger.Info("Forwarder started - listening on port %d, forwarding to %s",
		f.cfg.Network.LocalListenPort, f.cfg.GetAddress())
	return nil
}

// Stop stops the forwarder
func (f *Forwarder) Stop() {
	logger.Info("Stopping forwarder...")
	close(f.stopCh)

	// Stop authentication client
	if f.authClient != nil {
		f.authClient.Stop()
	}

	f.listenerNode.Close()
	f.senderNode.Close()
	logger.Info("Forwarder stopped")
}

// receiveAndForward listens for incoming MAVLink messages from Pixhawk and forwards them to server
func (f *Forwarder) receiveAndForward() {
	eventCh := f.listenerNode.Events()
	messageCount := 0
	forwardedCount := 0

	for {
		select {
		case <-f.stopCh:
			return
		case event := <-eventCh:
			now := time.Now()
			switch e := event.(type) {
			case *gomavlib.EventFrame:
				// Received a MAVLink message from Pixhawk
				msg := e.Message()
				msgTypeName := getMessageTypeName(msg)
				sysID := e.SystemID()
				seqNum := e.Frame.GetSequenceNumber()
				messageCount++

				// Skip messages not from Pixhawk (filter by SystemID 1, or from our own GCS)
				// Only forward messages from flight controller (typically SystemID 1)
				if sysID == 255 {
					// Skip GCS messages (our own heartbeats)
					logger.Debug("[SKIP] GCS message %s (SysID: %d)", msgTypeName, sysID)
					continue
				}

				// Deduplicate messages by checking sequence number
				f.seqMu.Lock()
				lastSeq, exists := f.lastSeqNum[sysID]
				if exists && lastSeq == seqNum {
					// Duplicate message, skip
					f.seqMu.Unlock()
					logger.Debug("[DUP] Skipping duplicate %s (SysID: %d, Seq: %d)", msgTypeName, sysID, seqNum)
					continue
				}
				f.lastSeqNum[sysID] = seqNum
				f.seqMu.Unlock()

				forwardedCount++

				// Debug: Log all received messages
				logger.Debug("[RX] %s (SysID: %d, Seq: %d)", msgTypeName, sysID, seqNum)

				if forwardedCount%10000 == 0 {
					logger.Info("[STATS] Forwarded %d messages (received %d, dedup rate: %.1f%%)",
						forwardedCount, messageCount, float64(messageCount-forwardedCount)/float64(messageCount)*100)
				}

				// Log specific message types at INFO level (reduced frequency)
				switch m := msg.(type) {
				case *common.MessageHeartbeat:
					if now.Sub(f.lastHeartbeatLog) > 30*time.Second {
						logger.Info("[PIXHAWK] Heartbeat: Type=%d, Mode=%d, Status=%d", m.Type, m.BaseMode, m.SystemStatus)
						f.lastHeartbeatLog = now
					}
					// Notify web server of connected Pixhawk
					web.HandleHeartbeat(sysID)
				case *common.MessageGpsRawInt:
					if now.Sub(f.lastGPSLog) > 30*time.Second {
						logger.Info("[PIXHAWK] GPS: Fix=%d, Lat=%.6f, Lon=%.6f, Sats=%d",
							m.FixType, float64(m.Lat)/1e7, float64(m.Lon)/1e7, m.SatellitesVisible)
						f.lastGPSLog = now
					}
				case *common.MessageSysStatus:
					if now.Sub(f.lastAttitudeLog) > 30*time.Second {
						logger.Info("[PIXHAWK] Status: Voltage=%.2fV, Battery=%d%%",
							float64(m.VoltageBattery)/1000, m.BatteryRemaining)
						f.lastAttitudeLog = now
					}
				case *common.MessageParamValue:
					// Forward to web server for parameter caching
					web.HandleParamValue(m)
					logger.Debug("[PARAM] %s = %v (%d/%d)", m.ParamId, m.ParamValue, m.ParamIndex, m.ParamCount)
				}

				// Forward message to server
				f.mu.RLock()
				healthy := f.isHealthy
				f.mu.RUnlock()

				if !healthy {
					metrics.Global.IncFailedUnhealthy(msgTypeName)
				} else {
					// Forward the raw frame directly to preserve original message
					if err := f.senderNode.WriteFrameAll(e.Frame); err != nil {
						logger.Error("[FORWARD] Failed to forward frame %s: %v", msgTypeName, err)
						metrics.Global.IncFailedSend(msgTypeName)
					} else {
						logger.Debug("[FORWARD] %s #%d", msgTypeName, forwardedCount)
						metrics.Global.IncSent(msgTypeName)
					}
				}

			case *gomavlib.EventChannelOpen:
				logger.Info("[LISTENER] Channel opened: %v", e.Channel)
			case *gomavlib.EventChannelClose:
				logger.Warn("[LISTENER] Channel closed: %v", e.Channel)
			case *gomavlib.EventParseError:
				logger.Debug("[LISTENER] Parse error: %v", e.Error)
			}
		}
	}
}

// receiveFromServer listens for incoming MAVLink messages from server and logs them
func (f *Forwarder) receiveFromServer() {
	eventCh := f.senderNode.Events()

	for {
		select {
		case <-f.stopCh:
			return
		case event := <-eventCh:
			switch e := event.(type) {
			case *gomavlib.EventFrame:
				// Received a MAVLink message from server
				msg := e.Message()
				msgTypeName := getMessageTypeName(msg)

				logger.Debug("[SERVER->PIXHAWK] %s (SysID: %d)", msgTypeName, e.SystemID())

				// Forward message to Pixhawk
				if err := f.listenerNode.WriteMessageAll(msg); err != nil {
					logger.Error("[SERVER->PIXHAWK] Failed to forward %s: %v", msgTypeName, err)
				} else {
					logger.Debug("[SERVER->PIXHAWK] Forwarded %s", msgTypeName)
				}

			case *gomavlib.EventChannelOpen:
				logger.Info("[SENDER] Channel opened: %v", e.Channel)
			case *gomavlib.EventChannelClose:
				logger.Warn("[SENDER] Channel closed: %v", e.Channel)
			case *gomavlib.EventParseError:
				logger.Debug("[SENDER] Parse error: %v", e.Error)
			}
		}
	}
}
func (f *Forwarder) sendHeartbeat() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-f.stopCh:
			return
		case <-ticker.C:
			msg := &common.MessageHeartbeat{
				Type:         6, // MAV_TYPE_GCS
				Autopilot:    0, // MAV_AUTOPILOT_INVALID
				BaseMode:     0, // MAV_MODE_FLAG enum
				CustomMode:   0,
				SystemStatus: 4, // MAV_STATE_ACTIVE
			}
			if err := f.listenerNode.WriteMessageAll(msg); err != nil {
				logger.Error("[HEARTBEAT] Failed to send GCS heartbeat: %v", err)
			} else {
				logger.Debug("[HEARTBEAT] Sent GCS heartbeat")
			}
		}
	}
}

// sendMavlinkSessionHeartbeat sends SESSION_HEARTBEAT messages with session token to sync IP:Port
// This ensures the UDP source port matches between heartbeat and MAVLink data
func (f *Forwarder) sendMavlinkSessionHeartbeat() {
	if f.authClient == nil {
		logger.Warn("[MAVLINK_HB] No auth client, skipping MAVLink session heartbeat")
		return
	}

	// Get frequency from config (Hz)
	frequency := f.cfg.Auth.SessionHeartbeatFrequency
	if frequency <= 0 {
		frequency = 1.0 // Default 1 Hz
	}
	interval := time.Duration(1.0 / frequency * float64(time.Second))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("[MAVLINK_HB] Starting MAVLink session heartbeat at %.1f Hz", frequency)
	firstSent := false
	sequence := uint16(0)

	for {
		select {
		case <-f.stopCh:
			return
		case <-ticker.C:
			tokenHex, expiresAt := f.authClient.GetSessionInfo()
			if tokenHex == "" {
				continue // No session yet
			}

			// Convert hex token to binary (32 bytes)
			var tokenBinary [32]byte
			if len(tokenHex) >= 64 {
				// Decode first 64 hex chars to 32 bytes
				for i := 0; i < 32; i++ {
					fmt.Sscanf(tokenHex[i*2:i*2+2], "%02x", &tokenBinary[i])
				}
			} else {
				logger.Warn("[MAVLINK_HB] Token too short: %d chars", len(tokenHex))
				continue
			}

			// Create custom SESSION_HEARTBEAT message
			msg := &mavlink_custom.MessageSessionHeartbeat{
				Token:     tokenBinary,
				ExpiresAt: uint32(expiresAt.Unix()),
				Sequence:  sequence,
			}
			sequence++

			// Send via senderNode (to server) - this ensures same source port as MAVLink data
			if err := f.senderNode.WriteMessageAll(msg); err != nil {
				logger.Error("[MAVLINK_HB] Failed to send session heartbeat: %v", err)
			} else {
				if !firstSent {
					logger.Info("[MAVLINK_HB] ✓ First MAVLink session heartbeat sent (ID 42000)")
					firstSent = true
					// Signal that heartbeat is ready
					select {
					case f.udpHeartbeatSent <- struct{}{}:
					default:
					}
				}
				logger.Debug("[MAVLINK_HB] Sent session heartbeat #%d", sequence-1)
			}
		}
	}
}

func (f *Forwarder) monitorIPChange() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	checkIP := func() {
		currentIP, err := getLocalIP()
		if err != nil {
			logger.Debug("[IP_MONITOR] Failed to get IP: %v", err)
			return
		}

		if f.previousIP == "" {
			f.previousIP = currentIP
			metrics.Global.SetIP(currentIP)
			logger.Info("[IP_MONITOR] Initial IP: %s", currentIP)
			metrics.Global.AddLog("INFO", fmt.Sprintf("Initial IP: %s", currentIP))

			f.mu.Lock()
			f.isHealthy = true
			f.mu.Unlock()
		} else if f.previousIP != currentIP {
			logger.Warn("[IP_MONITOR] IP changed: %s -> %s - Reconnecting", f.previousIP, currentIP)
			metrics.Global.AddLog("WARN", fmt.Sprintf("IP changed: %s -> %s", f.previousIP, currentIP))
			metrics.Global.SetIP(currentIP)
			f.previousIP = currentIP

			// Close current sender node
			f.senderNode.Close()

			// Create new sender node with custom dialect (including SESSION_HEARTBEAT)
			node, err := gomavlib.NewNode(gomavlib.NodeConf{
				Endpoints: []gomavlib.EndpointConf{
					gomavlib.EndpointUDPClient{Address: f.cfg.GetAddress()},
				},
				Dialect:     mavlink_custom.GetCombinedDialect(),
				OutVersion:  gomavlib.V2,
				OutSystemID: 1, // Not actually used since we forward raw frames
			})
			if err != nil {
				logger.Error("[IP_MONITOR] Error recreating sender node: %v", err)
				return
			}

			f.senderNode = node
			logger.Info("[IP_MONITOR] Sender reconnected on IP: %s", currentIP)

			// Also force TCP auth client to reconnect immediately
			if f.authClient != nil {
				f.authClient.ForceReconnect()
			}

			f.mu.Lock()
			f.isHealthy = true
			f.mu.Unlock()
		}
	}

	for {
		select {
		case <-f.stopCh:
			return
		case <-ticker.C:
			checkIP()
		case <-f.forceCheckCh:
			checkIP()
		}
	}
}
