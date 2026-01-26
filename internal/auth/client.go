package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"DroneBridge/internal/metrics"
)

// Client handles drone authentication with the router
type Client struct {
	host              string
	port              int
	droneUUID         string // UUID from drones_v2 table
	sharedSecret      string // Shared secret for registration
	secret            string // Loaded secret key (in-memory cache)
	configPath        string // Path to config file for saving updates
	keepaliveInterval time.Duration
	sessionToken      string
	expiresAt         time.Time
	refreshInterval   time.Duration // Server-recommended refresh interval

	conn              net.Conn
	running           bool
	stopCh            chan struct{}
	mu                sync.RWMutex
	tcpMu             sync.Mutex // For synchronizing TCP operations
	reconnectDelay    time.Duration
	previousLocalIP   string        // Track previous local IP for change detection
	lastIPChangeTime  time.Time     // Track last IP change time
	ipChangeThreshold time.Duration // Minimum time between IP changes before retrying refresh

	// API Key management channels
	apiKeyRespCh        chan *APIKeyResponse
	apiKeyRevokeAckCh   chan *APIKeyRevokeAck
	apiKeyStatusCh      chan *APIKeyStatusResponse
	apiKeyDeleteAckCh   chan *APIKeyDeleteAck
	sessionRefreshAckCh chan *SessionRefreshAck

	OnNetworkError func() // Callback when network error is detected
}

// NewClient creates a new authentication client using UUID-based protocol
func NewClient(host string, port int, droneUUID string, sharedSecret string, keepaliveInterval int) *Client {
	// If UUID is empty, try to get or generate one
	if droneUUID == "" {
		droneUUID = getOrGenerateUUID()
		log.Printf("[AUTH] No UUID provided in config, using auto-generated: %s", droneUUID)
	}

	return &Client{
		host:                host,
		port:                port,
		droneUUID:           droneUUID,
		sharedSecret:        sharedSecret,
		secret:              "",            // Will be loaded on demand
		configPath:          "config.yaml", // Default config path
		keepaliveInterval:   time.Duration(keepaliveInterval) * time.Second,
		stopCh:              make(chan struct{}),
		reconnectDelay:      5 * time.Second,
		ipChangeThreshold:   10 * time.Second,
		apiKeyRespCh:        make(chan *APIKeyResponse, 1),
		apiKeyRevokeAckCh:   make(chan *APIKeyRevokeAck, 1),
		apiKeyStatusCh:      make(chan *APIKeyStatusResponse, 1),
		apiKeyDeleteAckCh:   make(chan *APIKeyDeleteAck, 1),
		sessionRefreshAckCh: make(chan *SessionRefreshAck, 1),
	}
}

// getOrGenerateUUID attempts to retrieve a persistent UUID for this drone
func getOrGenerateUUID() string {
	uuidFile := ".drone_uuid"

	// 1. Try to read from file
	data, err := os.ReadFile(uuidFile)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id
		}
	}

	// 2. Try to generate from MAC address
	id := getIDFromMAC()
	if id == "" {
		// 3. Fallback to random UUID
		id = generateRandomUUID()
	}

	// Save for next time
	os.WriteFile(uuidFile, []byte(id), 0644)
	return id
}

func getIDFromMAC() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback == 0 && iface.HardwareAddr != nil {
			mac := iface.HardwareAddr.String()
			if mac != "" {
				// Format: 00:11:22:33:44:55 -> 00112233-4455-0000-0000-000000000000 (just an example)
				cleanMAC := strings.ReplaceAll(mac, ":", "")
				if len(cleanMAC) >= 12 {
					return fmt.Sprintf("%s-%s-%s-%s-%s",
						cleanMAC[:8], cleanMAC[8:12], "5555", "8888", "999999999999")
				}
			}
		}
	}
	return ""
}

func generateRandomUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "static-fallback-uuid-0001"
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Start begins authentication and keepalive
func (c *Client) Start() error {
	c.mu.RLock()
	if c.running {
		c.mu.RUnlock()
		log.Printf("[AUTH] ⓘ Auth client already running, skipping Start()")
		return nil
	}
	c.mu.RUnlock()

	log.Printf("[AUTH] Starting authentication client for drone UUID=%s", c.droneUUID)

	// Check if already have valid session from REGISTER
	c.mu.RLock()
	hasValidSession := c.sessionToken != "" && time.Now().Before(c.expiresAt)
	c.mu.RUnlock()

	if hasValidSession {
		// Session created during REGISTER - just start keepalive
		log.Printf("[AUTH] ✓ Valid session from REGISTER flow, starting keepalive")
	} else {
		// No session yet - perform AUTH
		err := c.authenticate()
		if err != nil {
			return fmt.Errorf("initial authentication failed: %w", err)
		}
	}

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	// Start keepalive goroutine
	go c.keepaliveLoop()

	log.Printf("[AUTH] ✅ Authenticated - keepalive active every %.0fs", c.keepaliveInterval.Seconds())
	return nil
}

// Register performs the one-time registration process
// Flow: REGISTER_INIT(UUID) → REGISTER_CHALLENGE → REGISTER_RESPONSE(HMAC-Shared) → REGISTER_ACK(Secret+Session)
func (c *Client) Register() error {
	if c.sharedSecret == "" {
		return fmt.Errorf("shared secret is required for registration")
	}

	log.Printf("[REGISTER] Starting registration for drone UUID=%s...", c.droneUUID)
	log.Printf("[REGISTER] Connecting to %s:%d...", c.host, c.port)

	// Connect to auth server
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", c.host, c.port), 10*time.Second)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	// DO NOT defer conn.Close() - we want to keep this connection alive!

	// Enable TCP keepalive to prevent disconnects
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
		log.Printf("[REGISTER] ✓ TCP keepalive enabled (30s interval)")
	}

	// Step 1: Send REGISTER_INIT
	init := &RegisterInit{
		DroneUUID: c.droneUUID,
	}

	packet := SerializeRegisterInit(init)
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("failed to send REGISTER_INIT: %w", err)
	}
	log.Printf("[REGISTER] ✓ Sent REGISTER_INIT")

	// Step 2: Receive REGISTER_CHALLENGE
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to receive REGISTER_CHALLENGE: %w", err)
	}

	challenge, err := ParseRegisterChallenge(buf[:n])
	if err != nil {
		return fmt.Errorf("failed to parse REGISTER_CHALLENGE: %w", err)
	}
	log.Printf("[REGISTER] ✓ Received challenge")

	// Step 3: Compute HMAC with SHARED SECRET
	timestamp := uint64(time.Now().Unix())
	hmacSig := ComputeHMAC(c.sharedSecret, c.droneUUID, challenge.Nonce, timestamp)

	// Step 4: Send REGISTER_RESPONSE
	resp := &RegisterResponse{
		DroneUUID: c.droneUUID,
		HMAC:      hmacSig,
		Timestamp: timestamp,
	}

	packet = SerializeRegisterResponse(resp)
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("failed to send REGISTER_RESPONSE: %w", err)
	}
	log.Printf("[REGISTER] ✓ Sent REGISTER_RESPONSE")

	// Step 5: Receive REGISTER_ACK with SECRET (no session)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to receive REGISTER_ACK: %w", err)
	}

	ack, err := ParseRegisterAck(buf[:n])
	if err != nil {
		return fmt.Errorf("failed to parse REGISTER_ACK: %w", err)
	}

	if ack.Result != ResultSuccess {
		return fmt.Errorf("registration failed (error=%d)", ack.ErrorCode)
	}

	log.Printf("[REGISTER] ✅ Registration successful!")
	log.Printf("[REGISTER] 🔑 Received SECRET KEY (len=%d)", len(ack.SecretKey))

	// Step 6: Save Secret Key
	if err := SaveSecret(c.droneUUID, ack.SecretKey); err != nil {
		return fmt.Errorf("failed to save secret key: %w", err)
	}
	log.Printf("[REGISTER] 💾 Secret key saved to '%s'", SecretFileName)

	// Step 7: Close connection - session will be obtained via AUTH flow
	// This prevents duplicate session creation issue
	conn.Close()
	log.Printf("[REGISTER] ✅ Registration complete. Connection closed.")

	c.mu.Lock()
	c.secret = ack.SecretKey
	c.conn = nil        // Connection closed, will reconnect during AUTH
	c.sessionToken = "" // No session from REGISTER (will get from AUTH)
	c.expiresAt = time.Time{}
	c.mu.Unlock()

	log.Printf("[REGISTER] ℹ️  Session will be obtained during AUTH flow (Start())")

	return nil
}

// computeCombinedKey generates a combined key from shared and private keys
// Logic: SHA256(shared_key + private_key) -> Hex String
func computeCombinedKey(sharedKey, privateKey string) string {
	hash := sha256.Sum256([]byte(sharedKey + privateKey))
	return hex.EncodeToString(hash[:])
}

// ComputeHMACWithKey removed - using ComputeHMAC from hmac.go

// Stop stops the authentication client
func (c *Client) Stop() {
	log.Println("[AUTH] Stopping authentication client...")

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	close(c.stopCh)

	if c.conn != nil {
		c.conn.Close()
	}

	log.Println("[AUTH] 👋 Authentication client stopped")
}

// IsAuthenticated returns true if the client has a valid session
func (c *Client) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.sessionToken != "" && time.Now().Before(c.expiresAt)
}

// authenticate performs the authentication handshake (UUID-based with Secret Key)
// Flow: AUTH_INIT(UUID) → AUTH_CHALLENGE → AUTH_RESPONSE(HMAC-Combined) → AUTH_ACK(Session)
func (c *Client) authenticate() error {
	// 1. Ensure we have secret key
	if c.secret == "" {
		// Try to load from storage
		uuid, key, err := LoadSecret()
		if err != nil {
			return fmt.Errorf("failed to load secret key (drone not registered?): %w. Run with --register first", err)
		}
		if uuid != c.droneUUID {
			log.Printf("[AUTH] Warn: Secret file UUID (%s) doesn't match config UUID (%s)", uuid, c.droneUUID)
		}
		c.secret = key
		log.Printf("[AUTH] Loaded secret key from storage")
	}

	// 2. Check if we already have a connection (from Register())
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		// No existing connection - create new one
		log.Printf("[AUTH] Connecting to %s:%d...", c.host, c.port)

		newConn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", c.host, c.port), 10*time.Second)
		if err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}

		// Enable TCP keepalive to prevent disconnects
		if tcpConn, ok := newConn.(*net.TCPConn); ok {
			tcpConn.SetKeepAlive(true)
			tcpConn.SetKeepAlivePeriod(30 * time.Second)
			log.Printf("[AUTH] ✓ TCP keepalive enabled (30s interval)")
		}

		c.mu.Lock()
		c.conn = newConn
		c.previousLocalIP = newConn.LocalAddr().(*net.TCPAddr).IP.String()
		c.lastIPChangeTime = time.Now()
		c.mu.Unlock()

		conn = newConn
		log.Printf("[AUTH] ✓ Connected from local IP: %s", c.previousLocalIP)
	} else {
		log.Printf("[AUTH] ✓ Reusing existing connection from REGISTER")
	}

	// Step 2: Send AUTH_INIT with UUID
	init := &AuthInit{
		DroneUUID: c.droneUUID,
	}

	packet := SerializeAuthInit(init)
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("failed to send AUTH_INIT: %w", err)
	}
	log.Printf("[AUTH] ✓ Sent AUTH_INIT (UUID=%s)", c.droneUUID)

	// Step 3: Receive AUTH_CHALLENGE
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to receive AUTH_CHALLENGE: %w", err)
	}

	challenge, err := ParseAuthChallenge(buf[:n])
	if err != nil {
		return fmt.Errorf("failed to parse AUTH_CHALLENGE: %w", err)
	}
	log.Printf("[AUTH] ✓ Received challenge")

	// Step 4: Compute HMAC (Combined Key = SHA256(Secret + Shared))
	// If shared secret is not configured, we might use just secret?
	// The requirement is "combined_key (derived from hash(secret_key + shared_secret))"
	// Backend expects combined key if shared secret is known.
	// If shared secret is missing in client config, this will fail if backend enforces combined key.
	// Assuming config has shared secret or it's empty.

	authKey := c.secret
	if c.sharedSecret != "" {
		authKey = computeCombinedKey(c.sharedSecret, c.secret)
		log.Printf("[AUTH] Using COMBINED KEY for authentication")
	} else {
		log.Printf("[AUTH] Warn: No shared secret in config, using RAW SECRET KEY")
	}

	timestamp := uint64(time.Now().Unix())
	hmacSig := ComputeHMAC(authKey, c.droneUUID, challenge.Nonce, timestamp)

	// Step 5: Send AUTH_RESPONSE
	resp := &AuthResponse{
		DroneUUID: c.droneUUID,
		HMAC:      hmacSig,
		Timestamp: timestamp,
		IP:        "0.0.0.0",
	}

	packet = SerializeAuthResponse(resp)
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("failed to send AUTH_RESPONSE: %w", err)
	}
	log.Printf("[AUTH] ✓ Sent AUTH_RESPONSE")

	// Step 6: Receive AUTH_ACK with SESSION
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to receive AUTH_ACK: %w", err)
	}

	ack, err := ParseAuthAck(buf[:n])
	if err != nil {
		return fmt.Errorf("failed to parse AUTH_ACK: %w", err)
	}

	if ack.Result != ResultSuccess {
		return fmt.Errorf("authentication failed (error=%d, wait=%ds)", ack.ErrorCode, ack.WaitSec)
	}

	// AUTH_ACK now contains session token directly
	if ack.SessionToken == "" {
		return fmt.Errorf("authentication successful but no session token received")
	}

	log.Printf("[AUTH] ✅ Authentication successful! (identity verified)")
	metrics.Global.SetAuthStatus("Authenticated")
	metrics.Global.AddLog("INFO", "Authentication successful - UUID: "+c.droneUUID)

	// Store session info
	c.mu.Lock()
	c.sessionToken = ack.SessionToken
	c.expiresAt = time.Unix(int64(ack.ExpiresAt), 0)
	c.refreshInterval = time.Duration(ack.Interval) * time.Second
	c.mu.Unlock()

	metrics.Global.SetSessionInfo(c.expiresAt, c.refreshInterval)

	log.Printf("[SESSION] ✅ Session ready!")
	log.Printf("[SESSION]    Token: %s...", c.sessionToken[:20])
	log.Printf("[SESSION]    Expires: %s", c.expiresAt.Format("2006-01-02 15:04:05"))

	return nil
}

// requestSession requests a session token from the server (after authentication)
func (c *Client) requestSession(conn net.Conn) error {
	log.Printf("[SESSION] 📋 Requesting session...")

	// Get old session token for potential reuse
	c.mu.RLock()
	oldToken := c.sessionToken
	c.mu.RUnlock()

	// Send SESSION_NEW request
	sessionReq := &SessionRequest{
		DroneUUID:       c.droneUUID,
		OldSessionToken: oldToken, // Server may reuse if still valid
	}

	packet := SerializeSessionRequest(sessionReq)
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("failed to send SESSION_NEW: %w", err)
	}
	log.Printf("[SESSION] ✓ Sent SESSION_NEW (UUID=%s, oldToken=%s)",
		c.droneUUID, truncateToken(oldToken))

	// Receive SESSION_ACK
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to receive SESSION_ACK: %w", err)
	}

	sessionAck, err := ParseSessionAck(buf[:n])
	if err != nil {
		return fmt.Errorf("failed to parse SESSION_ACK: %w", err)
	}

	if sessionAck.Result != ResultSuccess {
		return fmt.Errorf("session request failed (error=%d)", sessionAck.ErrorCode)
	}

	// Store session info
	c.mu.Lock()
	c.sessionToken = sessionAck.Token
	c.expiresAt = time.Unix(int64(sessionAck.ExpiresAt), 0)
	c.refreshInterval = time.Duration(sessionAck.Interval) * time.Second
	c.mu.Unlock()

	// Update metrics
	metrics.Global.SetSessionInfo(c.expiresAt, c.refreshInterval)

	log.Printf("[SESSION] ✅ Session ready!")
	log.Printf("[SESSION]    Token: %s...", c.sessionToken[:20])
	log.Printf("[SESSION]    Expires: %s", c.expiresAt.Format("2006-01-02 15:04:05"))
	log.Printf("[SESSION]    Refresh Interval: %.0fs", c.refreshInterval.Seconds())

	return nil
}

// truncateToken returns first 20 chars of token or "none"
func truncateToken(token string) string {
	if token == "" {
		return "none"
	}
	if len(token) > 20 {
		return token[:20] + "..."
	}
	return token
}

// RefreshError represents an error from session refresh with error code
type RefreshError struct {
	Message   string
	ErrorCode uint8 // 0 = no specific code, otherwise see ErrInvalidToken, ErrSessionExpired etc.
}

func (e *RefreshError) Error() string {
	return e.Message
}

// sendRefresh sends SESSION_REFRESH to extend session
// Returns RefreshError with ErrorCode if server rejects the refresh
func (c *Client) sendRefresh() error {
	c.tcpMu.Lock() // 🔒 Lock for entire send+receive cycle
	defer c.tcpMu.Unlock()

	c.mu.RLock()
	token := c.sessionToken
	conn := c.conn
	timeSinceIPChange := time.Since(c.lastIPChangeTime)
	c.mu.RUnlock()

	// Skip refresh if IP changed too recently (avoid re-auth loop)
	if timeSinceIPChange < c.ipChangeThreshold {
		log.Printf("[SESSION_REFRESH] ⏭️ Skipping (IP changed %v ago, threshold: %v)", timeSinceIPChange, c.ipChangeThreshold)
		return nil
	}

	if token == "" {
		return &RefreshError{Message: "no session token", ErrorCode: ErrInvalidToken}
	}

	// Reconnect if connection lost
	if conn == nil {
		log.Printf("[SESSION_REFRESH] Connection lost, attempting to reconnect...")
		if err := c.reconnectTCP(); err != nil {
			return &RefreshError{Message: fmt.Sprintf("failed to reconnect: %v", err)}
		}
		c.mu.RLock()
		conn = c.conn
		c.mu.RUnlock()
	}

	// Send SESSION_REFRESH
	refreshReq := &SessionRefreshRequest{
		SessionToken: token,
		DroneUUID:    c.droneUUID,
	}

	packet := SerializeSessionRefresh(refreshReq)
	if _, err := conn.Write(packet); err != nil {
		return &RefreshError{Message: fmt.Sprintf("failed to send SESSION_REFRESH: %v", err)}
	}
	log.Printf("[SESSION_REFRESH] ✓ Sent SESSION_REFRESH")

	// Receive SESSION_REFRESH_ACK - use shorter timeout to avoid blocking other operations
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	conn.SetReadDeadline(time.Time{}) // Reset deadline

	if err != nil {
		return &RefreshError{Message: fmt.Sprintf("failed to receive SESSION_REFRESH_ACK: %v", err)}
	}

	ackResp, err := ParseSessionRefreshAck(buf[:n])
	if err != nil {
		return &RefreshError{Message: fmt.Sprintf("failed to parse SESSION_REFRESH_ACK: %v", err)}
	}

	if ackResp.Result != ResultSuccess {
		return &RefreshError{
			Message:   fmt.Sprintf("session refresh rejected (error=%d)", ackResp.ErrorCode),
			ErrorCode: ackResp.ErrorCode,
		}
	}

	// Update expiration
	c.mu.Lock()
	c.expiresAt = time.Unix(int64(ackResp.ExpiresAt), 0)
	refreshInterval := c.refreshInterval
	c.mu.Unlock()

	// Update metrics
	metrics.Global.SetSessionInfo(c.expiresAt, refreshInterval)

	log.Printf("[SESSION_REFRESH] ✓ Session extended (expires: %s)",
		time.Unix(int64(ackResp.ExpiresAt), 0).Format("15:04:05"))

	return nil
}

// keepaliveLoop runs periodic keepalive messages - TCP session refresh
func (c *Client) keepaliveLoop() {
	// Start refresh ticker with server-recommended interval (default to 30s if not set)
	c.mu.RLock()
	refreshInterval := c.refreshInterval
	if refreshInterval == 0 {
		refreshInterval = 30 * time.Second
	}
	c.mu.RUnlock()

	refreshTicker := time.NewTicker(refreshInterval)
	defer refreshTicker.Stop()

	log.Printf("[KEEPALIVE] Starting refresh every %.0fs", refreshInterval.Seconds())

	for {
		select {
		case <-c.stopCh:
			return

		case <-refreshTicker.C:
			// Send TCP refresh to maintain session
			c.mu.RLock()
			running := c.running
			c.mu.RUnlock()

			if running {
				if err := c.sendRefresh(); err != nil {
					log.Printf("[REFRESH] ❌ Failed: %v", err)

					// Check if this is a RefreshError with specific error code
					var needReauth bool
					var isNetworkError bool
					if refreshErr, ok := err.(*RefreshError); ok {
						// Session not found or expired on server - MUST re-authenticate
						if refreshErr.ErrorCode == ErrInvalidToken || refreshErr.ErrorCode == ErrSessionExpired {
							log.Printf("[REFRESH] ⚠️ Session invalid on server (error=%d) - need full re-auth", refreshErr.ErrorCode)
							needReauth = true
						} else {
							// Any other error is likely network-related
							isNetworkError = true
						}
					} else {
						// No error code means it's a network error
						isNetworkError = true
					}

					// Only close connection on network errors, NOT on SESSION_REFRESH rejection
					// Rejection means the connection is still alive, just session issue
					if isNetworkError {
						log.Printf("[REFRESH] 🔌 Network error detected, closing connection for clean reconnect")
						c.mu.Lock()
						if c.conn != nil {
							c.conn.Close()
							c.conn = nil
						}
						c.mu.Unlock()
					}

					if needReauth {
						// Session not found on server - re-authenticate immediately
						log.Printf("[REFRESH] 🔄 Re-authenticating (session not found on server)...")
						if err := c.authenticate(); err != nil {
							log.Printf("[AUTH] ❌ Re-authentication failed: %v", err)
						} else {
							log.Printf("[AUTH] ✅ Re-authentication successful - Session recovered!")
						}
					} else if isNetworkError {
						// Network error - try reconnecting TCP (reuse token if still valid locally)
						c.mu.RLock()
						tokenValid := c.sessionToken != "" && time.Now().Before(c.expiresAt)
						c.mu.RUnlock()

						if tokenValid {
							log.Printf("[REFRESH] 🔄 Token still valid locally, reconnecting TCP...")
							if err := c.reconnectTCP(); err != nil {
								log.Printf("[REFRESH] ❌ TCP reconnect failed: %v - re-authenticating", err)
								if err := c.authenticate(); err != nil {
									log.Printf("[AUTH] ❌ Authentication failed: %v", err)
								} else {
									log.Printf("[AUTH] ✅ Authentication successful - Session recovered!")
								}
							} else {
								log.Printf("[REFRESH] ✅ TCP reconnected, will retry refresh next cycle")
							}
						} else {
							log.Printf("[REFRESH] ⚠️ Token expired, re-authenticating...")
							if err := c.authenticate(); err != nil {
								log.Printf("[AUTH] ❌ Re-authentication failed: %v", err)
							} else {
								log.Printf("[AUTH] ♻️ Re-authentication successful - Session recovered!")
							}
						}
					}
				}
			}
		}
	}
}

// TriggerReauth performs immediate re-authentication (for session recovery)
// This does full auth + session request
func (c *Client) TriggerReauth() error {
	log.Printf("[REAUTH] 🔄 Triggering immediate re-authentication...")
	return c.authenticate()
}

// TriggerSessionRecovery attempts session refresh first, falls back to re-auth if needed
// Key insight: Session can be reused across IP changes if still valid
func (c *Client) TriggerSessionRecovery() error {
	c.mu.RLock()
	token := c.sessionToken
	expiresAt := c.expiresAt
	running := c.running
	conn := c.conn
	c.mu.RUnlock()

	if !running {
		return fmt.Errorf("auth client not running")
	}

	// Check if we have a valid session that can be refreshed
	if token != "" && time.Now().Before(expiresAt) {
		log.Printf("[SESSION_RECOVERY] 🔄 Session still valid (expires %s), attempting refresh...",
			expiresAt.Format("15:04:05"))

		// Try session refresh first (just extend TTL, keep same token)
		if err := c.sendRefresh(); err != nil {
			log.Printf("[SESSION_RECOVERY] ⚠️ Session refresh failed: %v", err)

			// If refresh fails but session still valid, try requesting new session
			// (re-auth may not be needed if TCP connection still valid)
			if conn != nil {
				log.Printf("[SESSION_RECOVERY] 🔄 Trying to request new session on existing TCP...")
				if err := c.requestSession(conn); err != nil {
					log.Printf("[SESSION_RECOVERY] ⚠️ Session request failed: %v, falling back to full re-auth", err)
					return c.TriggerReauth()
				}
				log.Printf("[SESSION_RECOVERY] ✅ New session obtained on existing TCP connection")
				return nil
			}

			// No valid connection, do full re-auth
			log.Printf("[SESSION_RECOVERY] 🔄 No TCP connection, doing full re-auth...")
			return c.TriggerReauth()
		}
		log.Printf("[SESSION_RECOVERY] ✅ Session refreshed successfully")
		return nil
	}

	// Session expired or doesn't exist - need new session
	log.Printf("[SESSION_RECOVERY] ⚠️ No valid session, need new session...")

	// Try to request session on existing TCP if available
	if conn != nil {
		log.Printf("[SESSION_RECOVERY] 🔄 Trying to request session on existing TCP...")
		if err := c.requestSession(conn); err != nil {
			log.Printf("[SESSION_RECOVERY] ⚠️ Session request failed (TCP may need re-auth): %v", err)
			return c.TriggerReauth()
		}
		log.Printf("[SESSION_RECOVERY] ✅ Session obtained!")
		return nil
	}

	// No connection, do full re-auth
	log.Printf("[SESSION_RECOVERY] 🔄 No TCP connection, doing full re-auth...")
	return c.TriggerReauth()
}

// GetSessionInfo returns current session information
func (c *Client) GetSessionInfo() (token string, expiresAt time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionToken, c.expiresAt
}

// reconnectTCP attempts to reconnect the TCP connection to the auth server
func (c *Client) reconnectTCP() error {
	log.Printf("[RECONNECT] Attempting to reconnect TCP to %s:%d", c.host, c.port)

	// Close existing connection if any
	c.mu.RLock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.mu.RUnlock()

	// Create new connection
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", c.host, c.port), 10*time.Second)
	if err != nil {
		return fmt.Errorf("reconnection failed: %w", err)
	}

	// Enable TCP keepalive to prevent disconnects
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		// Set keepalive
		tcpConn.SetKeepAlive(true)
		// Keepalive settings: send keepalive after 60s idle, every 30s, 3 probes
		tcpConn.SetKeepAlivePeriod(30 * time.Second) // Go 1.7+
		log.Printf("[RECONNECT] ✓ TCP keepalive enabled (30s interval)")
	}

	c.mu.Lock()
	c.conn = conn
	currentLocalIP := conn.LocalAddr().(*net.TCPAddr).IP.String()
	if c.previousLocalIP != "" && c.previousLocalIP != currentLocalIP {
		msg := fmt.Sprintf("TCP Local IP changed from %s to %s", c.previousLocalIP, currentLocalIP)
		log.Printf("[IP_CHANGE] 🔄 %s", msg)
		metrics.Global.AddLog("WARN", msg)
		c.lastIPChangeTime = time.Now() // Record IP change time to skip next refresh
	}
	c.previousLocalIP = currentLocalIP
	metrics.Global.SetIP(currentLocalIP)
	c.mu.Unlock()

	log.Printf("[RECONNECT] ✓ TCP reconnected successfully from local IP: %s", currentLocalIP)
	return nil
}

// ForceReconnect closes the current connection to trigger an immediate reconnect
func (c *Client) ForceReconnect() {
	c.tcpMu.Lock()
	defer c.tcpMu.Unlock()

	if c.conn != nil {
		log.Println("[AUTH] ForceReconnect: Forcing TCP reconnection due to network change...")
		c.conn.Close()
		c.conn = nil
	}
}
