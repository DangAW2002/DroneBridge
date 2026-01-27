package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"

	"DroneBridge/internal/auth"
	"DroneBridge/internal/metrics"
)

//go:embed static/*
var staticFiles embed.FS

// XML content cache for parameter editor
var xmlContent []byte
var xmlOnce sync.Once

// ParamSetRequest represents a request to set a parameter
type ParamSetRequest struct {
	ParamName  string  `json:"paramName"`
	ParamValue float64 `json:"paramValue"`
	ParamType  string  `json:"paramType"`
}

// ParamSetResponse represents the response from setting a parameter
type ParamSetResponse struct {
	Success   bool    `json:"success"`
	Message   string  `json:"message"`
	ParamName string  `json:"paramName"`
	NewValue  float64 `json:"newValue,omitempty"`
}

// ConnectionStatus represents the current connection state
type ConnectionStatus struct {
	Connected bool   `json:"connected"`
	SystemID  uint8  `json:"systemId"`
	Message   string `json:"message"`
}

// CachedParameter represents a parameter with its current value from Pixhawk
type CachedParameter struct {
	ParamId    string  `json:"paramId"`
	ParamValue float64 `json:"paramValue"`
	ParamType  int     `json:"paramType"`
	ParamIndex uint16  `json:"paramIndex"`
}

// ParameterListStatus represents the status of parameter loading
type ParameterListStatus struct {
	Loading       bool              `json:"loading"`
	TotalCount    int               `json:"totalCount"`
	ReceivedCount int               `json:"receivedCount"`
	Progress      float64           `json:"progress"`
	Parameters    []CachedParameter `json:"parameters,omitempty"`
	LastUpdated   string            `json:"lastUpdated,omitempty"`
}

// MAVLinkBridge handles MAVLink communication for parameter setting
type MAVLinkBridge struct {
	node            *gomavlib.Node
	pixhawkSysID    uint8
	connected       bool
	mutex           sync.RWMutex
	responseTimeout time.Duration

	// Parameter cache
	paramCache      map[string]CachedParameter
	paramCacheMutex sync.RWMutex
	paramTotal      int
	paramReceived   int
	paramLoading    bool
	paramLastUpdate time.Time

	// Channel to receive PARAM_VALUE messages from forwarder
	paramValueCh chan *common.MessageParamValue
}

var bridge *MAVLinkBridge
var bridgeOnce sync.Once

// Camera streamer management
var cameraCmd *exec.Cmd
var cameraMutex sync.Mutex

// startCameraStreamer starts the camera streamer automatically
func startCameraStreamer() {
	// Kill any existing camera process first
	exec.Command("sudo", "pkill", "-f", "camera_streamer.py").Run()
	time.Sleep(1 * time.Second)
	
	cameraMutex.Lock()
	defer cameraMutex.Unlock()

	if cameraCmd != nil && cameraCmd.Process != nil {
		log.Printf("[CAMERA] Camera streamer is already running")
		return
	}

	cameraCmd = exec.Command("python3", "/home/pi/HBQ_server_drone/Find_landing/camera_streamer.py")
	cameraCmd.Dir = "/home/pi/HBQ_server_drone/Find_landing"
	
	// Pipe output to logs
	cameraCmd.Stdout = os.Stdout
	cameraCmd.Stderr = os.Stderr
	
	err := cameraCmd.Start()
	if err != nil {
		log.Printf("[CAMERA] Failed to start camera streamer: %v", err)
		cameraCmd = nil
		return
	}

	log.Printf("[CAMERA] Camera streamer started automatically (PID: %d)", cameraCmd.Process.Pid)
	
	// Monitor camera process
	go func() {
		err := cameraCmd.Wait()
		cameraMutex.Lock()
		cameraCmd = nil
		cameraMutex.Unlock()
		
		if err != nil {
			log.Printf("[CAMERA] Camera streamer exited with error: %v", err)
		} else {
			log.Printf("[CAMERA] Camera streamer stopped")
		}
	}()
}

// InitMAVLinkBridge initializes the MAVLink bridge with the given node
func InitMAVLinkBridge(node *gomavlib.Node) {
	bridgeOnce.Do(func() {
		bridge = &MAVLinkBridge{
			node:            node,
			responseTimeout: 5 * time.Second,
			paramCache:      make(map[string]CachedParameter),
			paramValueCh:    make(chan *common.MessageParamValue, 100),
		}
		go bridge.processParamValues()
	})
}

// HandleParamValue receives PARAM_VALUE message from forwarder
func HandleParamValue(msg *common.MessageParamValue) {
	if bridge != nil && bridge.paramValueCh != nil {
		select {
		case bridge.paramValueCh <- msg:
		default:
			// Channel full, skip
		}
	}
}

// HandleHeartbeat receives heartbeat from forwarder
func HandleHeartbeat(sysID uint8) {
	if bridge != nil {
		bridge.mutex.Lock()
		if !bridge.connected {
			bridge.pixhawkSysID = sysID
			bridge.connected = true
			log.Printf("[WEB] Connected to Pixhawk (System ID: %d)", sysID)
		}
		bridge.mutex.Unlock()
	}
}

func (b *MAVLinkBridge) processParamValues() {
	for msg := range b.paramValueCh {
		// Decode value based on type
		var decodedValue float64
		if msg.ParamType == common.MAV_PARAM_TYPE_INT32 ||
			msg.ParamType == common.MAV_PARAM_TYPE_UINT32 ||
			msg.ParamType == common.MAV_PARAM_TYPE_INT16 ||
			msg.ParamType == common.MAV_PARAM_TYPE_UINT16 ||
			msg.ParamType == common.MAV_PARAM_TYPE_INT8 ||
			msg.ParamType == common.MAV_PARAM_TYPE_UINT8 {
			decodedValue = float64(int32(math.Float32bits(msg.ParamValue)))
		} else {
			decodedValue = float64(msg.ParamValue)
		}

		b.paramCacheMutex.Lock()

		b.paramCache[msg.ParamId] = CachedParameter{
			ParamId:    msg.ParamId,
			ParamValue: decodedValue,
			ParamType:  int(msg.ParamType),
			ParamIndex: msg.ParamIndex,
		}

		b.paramTotal = int(msg.ParamCount)
		b.paramReceived = len(b.paramCache)
		b.paramLastUpdate = time.Now()

		// Check if loading complete
		if b.paramReceived >= b.paramTotal && b.paramLoading {
			b.paramLoading = false
			log.Printf("[WEB] Parameter loading complete: %d/%d parameters", b.paramReceived, b.paramTotal)
		}

		b.paramCacheMutex.Unlock()
	}
}

func (b *MAVLinkBridge) IsConnected() bool {
	if b == nil {
		return false
	}
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.connected
}

func (b *MAVLinkBridge) GetSystemID() uint8 {
	if b == nil {
		return 0
	}
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.pixhawkSysID
}

// GetPixhawkSystemID returns the actual Pixhawk system ID after connection is established
// This function should be used instead of hardcoding system IDs (like 1) because:
// 1. The actual system ID is detected from the Pixhawk heartbeat
// 2. Returns 1 as fallback if not yet connected (standard PX4 default)
// 3. Ensures all MAVLink operations use the correct, dynamic system ID
//
// Flow:
// - Forwarder receives heartbeat from Pixhawk -> captures sysID
// - Forwarder calls HandleHeartbeat(sysID) -> web bridge stores the ID
// - Web server can retrieve it via GetPixhawkSystemID() for parameter operations
// - Forwarder logs actual sysID for verification
func GetPixhawkSystemID() uint8 {
	if bridge == nil {
		return 1 // Fallback to default if bridge not initialized
	}
	return bridge.GetSystemID()
}

// RequestParameterList sends PARAM_REQUEST_LIST to Pixhawk
func (b *MAVLinkBridge) RequestParameterList() error {
	if b == nil || b.node == nil {
		return fmt.Errorf("MAVLink bridge not initialized")
	}

	b.mutex.RLock()
	connected := b.connected
	sysID := b.pixhawkSysID
	b.mutex.RUnlock()

	if !connected {
		return fmt.Errorf("not connected to Pixhawk")
	}

	// Clear cache and start loading
	b.paramCacheMutex.Lock()
	b.paramCache = make(map[string]CachedParameter)
	b.paramReceived = 0
	b.paramTotal = 0
	b.paramLoading = true
	b.paramCacheMutex.Unlock()

	// Create PARAM_REQUEST_LIST message
	msg := &common.MessageParamRequestList{
		TargetSystem:    sysID,
		TargetComponent: 1, // MAV_COMP_ID_AUTOPILOT1
	}

	log.Printf("[WEB] Sending PARAM_REQUEST_LIST to system %d", sysID)

	err := b.node.WriteMessageAll(msg)
	if err != nil {
		b.paramCacheMutex.Lock()
		b.paramLoading = false
		b.paramCacheMutex.Unlock()
		return fmt.Errorf("failed to send PARAM_REQUEST_LIST: %w", err)
	}

	return nil
}

// GetParameterListStatus returns the current status of parameter loading
func (b *MAVLinkBridge) GetParameterListStatus(includeParams bool) *ParameterListStatus {
	if b == nil {
		return &ParameterListStatus{Loading: false}
	}

	b.paramCacheMutex.RLock()
	defer b.paramCacheMutex.RUnlock()

	status := &ParameterListStatus{
		Loading:       b.paramLoading,
		TotalCount:    b.paramTotal,
		ReceivedCount: b.paramReceived,
	}

	if b.paramTotal > 0 {
		status.Progress = float64(b.paramReceived) / float64(b.paramTotal) * 100
	}

	if !b.paramLastUpdate.IsZero() {
		status.LastUpdated = b.paramLastUpdate.Format(time.RFC3339)
	}

	if includeParams && len(b.paramCache) > 0 {
		status.Parameters = make([]CachedParameter, 0, len(b.paramCache))
		for _, p := range b.paramCache {
			status.Parameters = append(status.Parameters, p)
		}
	}

	return status
}

// GetCachedParameter returns a single cached parameter value
func (b *MAVLinkBridge) GetCachedParameter(paramName string) (CachedParameter, bool) {
	if b == nil {
		return CachedParameter{}, false
	}

	b.paramCacheMutex.RLock()
	defer b.paramCacheMutex.RUnlock()

	param, exists := b.paramCache[paramName]
	return param, exists
}

func (b *MAVLinkBridge) SetParameter(paramName string, paramValue float64, paramType string) *ParamSetResponse {
	if b == nil || b.node == nil {
		return &ParamSetResponse{
			Success:   false,
			Message:   "MAVLink bridge not initialized",
			ParamName: paramName,
		}
	}

	b.mutex.RLock()
	connected := b.connected
	sysID := b.pixhawkSysID
	b.mutex.RUnlock()

	if !connected {
		return &ParamSetResponse{
			Success:   false,
			Message:   "Not connected to Pixhawk",
			ParamName: paramName,
		}
	}

	// Convert param type string to MAVLink type
	mavParamType := getMavParamType(paramType)

	// Encode the value based on type
	var encodedValue float32
	if mavParamType == common.MAV_PARAM_TYPE_INT32 || mavParamType == common.MAV_PARAM_TYPE_UINT32 ||
		mavParamType == common.MAV_PARAM_TYPE_INT16 || mavParamType == common.MAV_PARAM_TYPE_UINT16 ||
		mavParamType == common.MAV_PARAM_TYPE_INT8 || mavParamType == common.MAV_PARAM_TYPE_UINT8 {
		encodedValue = math.Float32frombits(uint32(int32(paramValue)))
	} else {
		encodedValue = float32(paramValue)
	}

	// Create PARAM_SET message
	paramMsg := &common.MessageParamSet{
		TargetSystem:    sysID,
		TargetComponent: 1,
		ParamId:         paramName,
		ParamValue:      encodedValue,
		ParamType:       mavParamType,
	}

	log.Printf("[WEB] Sending PARAM_SET: %s = %v (type: %s)", paramName, paramValue, paramType)

	err := b.node.WriteMessageAll(paramMsg)
	if err != nil {
		return &ParamSetResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to send PARAM_SET: %v", err),
			ParamName: paramName,
		}
	}

	// Wait for PARAM_VALUE response
	return b.waitForParamResponse(paramName)
}

func (b *MAVLinkBridge) waitForParamResponse(paramName string) *ParamSetResponse {
	timeout := time.After(b.responseTimeout)

	// Poll the cache for the updated value
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-ticker.C:
			b.paramCacheMutex.RLock()
			param, exists := b.paramCache[paramName]
			lastUpdate := b.paramLastUpdate
			b.paramCacheMutex.RUnlock()

			// Check if we got an update after sending the request
			if exists && lastUpdate.After(startTime) {
				log.Printf("[WEB] PARAM_VALUE received: %s = %v", paramName, param.ParamValue)
				return &ParamSetResponse{
					Success:   true,
					Message:   fmt.Sprintf("Parameter %s successfully set", paramName),
					ParamName: paramName,
					NewValue:  param.ParamValue,
				}
			}

		case <-timeout:
			return &ParamSetResponse{
				Success:   false,
				Message:   "Timeout waiting for parameter confirmation",
				ParamName: paramName,
			}
		}
	}
}

func getMavParamType(typeStr string) common.MAV_PARAM_TYPE {
	switch typeStr {
	case "FLOAT", "float", "REAL32":
		return common.MAV_PARAM_TYPE_REAL32
	case "INT32", "int":
		return common.MAV_PARAM_TYPE_INT32
	case "UINT32":
		return common.MAV_PARAM_TYPE_UINT32
	case "INT16":
		return common.MAV_PARAM_TYPE_INT16
	case "UINT16":
		return common.MAV_PARAM_TYPE_UINT16
	case "INT8":
		return common.MAV_PARAM_TYPE_INT8
	case "UINT8", "bool":
		return common.MAV_PARAM_TYPE_UINT8
	default:
		return common.MAV_PARAM_TYPE_INT32
	}
}

// formatUnixTimestamp converts Unix timestamp to ISO 8601 format
func formatUnixTimestamp(ts uint64) interface{} {
	if ts == 0 {
		return nil
	}
	return time.Unix(int64(ts), 0).Format(time.RFC3339)
}

func StartServer(port int, authClient *auth.Client, droneUUID string) {
	// Pre-load XML file into memory cache for faster serving
	loadXMLCache()

	// Serve static files with caching headers
	fsys, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal(err)
	}

	// Create a custom file server with caching headers
	fileServer := http.FileServer(http.FS(fsys))
	fileServerWithCache := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set cache headers for static files
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fileServer.ServeHTTP(w, r)
	})

	// Redirect root to dashboard
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/dashboard.html", http.StatusFound)
			return
		}
		// Set CORS and cache headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")

		if r.Method == http.MethodOptions {
			return
		}
		fileServerWithCache.ServeHTTP(w, r)
	})

	// API endpoint for status
	http.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		json.NewEncoder(w).Encode(metrics.Global.GetSnapshot())
	})

	// API endpoint for connection status
	http.HandleFunc("/api/connection", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")

		status := ConnectionStatus{
			Connected: false,
			SystemID:  0,
			Message:   "MAVLink bridge not initialized",
		}

		if bridge != nil {
			status.Connected = bridge.IsConnected()
			status.SystemID = bridge.GetSystemID()
			if status.Connected {
				status.Message = fmt.Sprintf("Connected to Pixhawk (System ID: %d)", status.SystemID)
			} else {
				status.Message = "Waiting for Pixhawk connection..."
			}
		}

		json.NewEncoder(w).Encode(status)
	})

	// API endpoint for setting parameters
	http.HandleFunc("/api/param/set", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ParamSetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
			return
		}

		log.Printf("[WEB] Received param set request: %+v", req)

		var response *ParamSetResponse
		if bridge != nil {
			response = bridge.SetParameter(req.ParamName, req.ParamValue, req.ParamType)
		} else {
			response = &ParamSetResponse{
				Success:   false,
				Message:   "MAVLink bridge not initialized",
				ParamName: req.ParamName,
			}
		}

		json.NewEncoder(w).Encode(response)
	})

	// API endpoint for health check
	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// API endpoint to request parameter list from Pixhawk
	http.HandleFunc("/api/param/request-list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if bridge == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "MAVLink bridge not initialized",
			})
			return
		}

		err := bridge.RequestParameterList()
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Parameter list request sent",
		})
	})

	// API endpoint to get parameter loading status and cached values
	http.HandleFunc("/api/param/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")

		includeParams := r.URL.Query().Get("include") == "params"

		if bridge == nil {
			json.NewEncoder(w).Encode(&ParameterListStatus{Loading: false})
			return
		}

		status := bridge.GetParameterListStatus(includeParams)
		json.NewEncoder(w).Encode(status)
	})

	// API endpoint to get all cached parameters
	http.HandleFunc("/api/param/list", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")

		if bridge == nil {
			json.NewEncoder(w).Encode([]CachedParameter{})
			return
		}

		status := bridge.GetParameterListStatus(true)
		json.NewEncoder(w).Encode(status.Parameters)
	})

	// API endpoint to get a single cached parameter
	http.HandleFunc("/api/param/get", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")

		paramName := r.URL.Query().Get("name")
		if paramName == "" {
			http.Error(w, "Missing 'name' parameter", http.StatusBadRequest)
			return
		}

		if bridge == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"found": false,
			})
			return
		}

		param, exists := bridge.GetCachedParameter(paramName)
		if !exists {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"found": false,
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"found": true,
			"param": param,
		})
	})

	// Helper function to set CORS headers
	setCORSHeaders := func(w http.ResponseWriter) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
	}

	// API Key Management Endpoints (compatible with HBQCONNECT format)
	// GET /api/v1/drone/api-key/status - Get current API key status
	http.HandleFunc("/api/v1/drone/api-key/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		setCORSHeaders(w)

		if r.Method == http.MethodOptions {
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if authClient == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Auth client not initialized",
			})
			return
		}

		// Try to get API key status with retries
		var state *auth.APIKeyStatusResponse
		var err error
		maxRetries := 3
		retryDelay := 500 * time.Millisecond

		for attempt := 0; attempt < maxRetries; attempt++ {
			state, err = authClient.GetAPIKeyStatus()
			if err == nil {
				break
			}
			if attempt < maxRetries-1 {
				time.Sleep(retryDelay)
			}
		}

		if err != nil {
			// Return a "no key" response instead of error if session is not ready
			// This allows frontend to gracefully show "no key" state
			json.NewEncoder(w).Encode(map[string]interface{}{
				"has_active_key": false,
				"status":         "none",
				"api_key":        nil,
				"error":          err.Error(),
			})
			return
		}

		// Convert response to frontend format
		json.NewEncoder(w).Encode(map[string]interface{}{
			"has_active_key": state.HasActiveKey == 0x01,
			"status":         state.Status,
			"api_key":        state.APIKey,
			"created_at":     formatUnixTimestamp(state.CreatedAt),
			"expires_at":     formatUnixTimestamp(state.ExpiresAt),
			"user_uuid":      state.UserUUID,
			"username":       nil, // TODO: Fetch username from backend DB if needed
			"user_active_at": formatUnixTimestamp(state.UserActivatedAt),
		})
	})

	// POST /api/v1/drone/api-key/request - Request new API key
	http.HandleFunc("/api/v1/drone/api-key/request", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		setCORSHeaders(w)

		if r.Method == http.MethodOptions {
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if authClient == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Auth client not initialized",
			})
			return
		}

		// Parse request body for expiration hours (optional)
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		expirationHours := 24 // Default
		if exp, ok := req["expiration_hours"]; ok {
			if v, ok := exp.(float64); ok {
				expirationHours = int(v)
			}
		}

		// Validate expiration range
		if expirationHours < 1 {
			expirationHours = 1
		}
		if expirationHours > 720 { // Max 30 days
			expirationHours = 720
		}

		state, err := authClient.RequestAPIKey(expirationHours)
		if err != nil {
			if err.Error() == "drone already has an active API key" {
				w.WriteHeader(http.StatusConflict)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
			return
		}

		// Convert response to frontend format
		json.NewEncoder(w).Encode(map[string]interface{}{
			"api_key":        state.APIKey,
			"created_at":     time.Now().Format(time.RFC3339),
			"expires_at":     formatUnixTimestamp(state.ExpiresAt),
			"user_uuid":      nil,
			"username":       nil,
			"user_active_at": nil,
		})
	})

	// DELETE /api/v1/drone/api-key/revoke - Revoke current API key
	http.HandleFunc("/api/v1/drone/api-key/revoke", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		setCORSHeaders(w)

		if r.Method == http.MethodOptions {
			return
		}

		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if authClient == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Auth client not initialized",
			})
			return
		}

		if err := authClient.RevokeAPIKey(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "API key revoked successfully",
		})
	})

	// DELETE /api/v1/drone/api-key/delete - Delete API key completely
	http.HandleFunc("/api/v1/drone/api-key/delete", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		setCORSHeaders(w)

		if r.Method == http.MethodOptions {
			return
		}

		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if authClient == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Auth client not initialized",
			})
			return
		}

		if err := authClient.DeleteAPIKey(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "API key deleted successfully",
		})
	})

	// API endpoint to list available landing templates
	http.HandleFunc("/api/landing/templates", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		setCORSHeaders(w)

		if r.Method == http.MethodOptions {
			return
		}

		templatesDir := "/home/pi/HBQ_server_drone/Find_landing/templates"
		files, err := os.ReadDir(templatesDir)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   err.Error(),
			})
			return
		}

		var templates []string
		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(strings.ToLower(file.Name()), ".png") {
				// Remove .png extension to get template name
				name := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))
				templates = append(templates, name)
			}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   true,
			"templates": templates,
		})
	})

	// Serve template images
	http.HandleFunc("/templates/", func(w http.ResponseWriter, r *http.Request) {
		templateName := strings.TrimPrefix(r.URL.Path, "/templates/")
		templatePath := filepath.Join("/home/pi/HBQ_server_drone/Find_landing/templates", templateName)
		
		// Security check - prevent directory traversal
		if strings.Contains(templateName, "..") {
			http.Error(w, "Invalid template name", http.StatusBadRequest)
			return
		}
		
		// Check if file exists
		if _, err := os.Stat(templatePath); os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, templatePath)
	})

	// API endpoint to start camera streamer
	http.HandleFunc("/api/camera/start", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		setCORSHeaders(w)

		if r.Method == http.MethodOptions {
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		cameraMutex.Lock()
		defer cameraMutex.Unlock()

		if cameraCmd != nil && cameraCmd.Process != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Camera streamer is already running",
			})
			return
		}

		cameraCmd = exec.Command("python3", "/home/pi/HBQ_server_drone/Find_landing/camera_streamer.py")
		cameraCmd.Dir = "/home/pi/HBQ_server_drone/Find_landing"
		err := cameraCmd.Start()
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": fmt.Sprintf("Failed to start camera: %v", err),
			})
			return
		}

		log.Printf("[CAMERA] Camera streamer started (PID: %d)", cameraCmd.Process.Pid)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Camera streamer started",
			"pid":     cameraCmd.Process.Pid,
		})
	})

	// API endpoint to stop camera streamer
	http.HandleFunc("/api/camera/stop", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		setCORSHeaders(w)

		if r.Method == http.MethodOptions {
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		cameraMutex.Lock()
		defer cameraMutex.Unlock()

		if cameraCmd == nil || cameraCmd.Process == nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Camera streamer is not running",
			})
			return
		}

		err := cameraCmd.Process.Kill()
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": fmt.Sprintf("Failed to stop camera: %v", err),
			})
			return
		}

		cameraCmd.Wait()
		cameraCmd = nil
		log.Printf("[CAMERA] Camera streamer stopped")

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Camera streamer stopped",
		})
	})

	// API endpoint to check camera status
	http.HandleFunc("/api/camera/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		setCORSHeaders(w)

		if r.Method == http.MethodOptions {
			return
		}

		cameraMutex.Lock()
		running := cameraCmd != nil && cameraCmd.Process != nil
		var pid int
		if running {
			pid = cameraCmd.Process.Pid
		}
		cameraMutex.Unlock()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"running": running,
			"pid":     pid,
		})
	})

	// API endpoint to save landing config
	http.HandleFunc("/api/landing/config/save", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		setCORSHeaders(w)

		if r.Method == http.MethodOptions {
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var config map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Invalid JSON: " + err.Error(),
			})
			return
		}

		configPath := "/home/pi/HBQ_server_drone/Find_landing/landing_config.json"
		configData, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Failed to marshal config: " + err.Error(),
			})
			return
		}

		if err := os.WriteFile(configPath, configData, 0644); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Failed to save config: " + err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Landing config saved successfully",
			"path":    configPath,
		})
	})

	// API endpoint to load landing config
	http.HandleFunc("/api/landing/config/load", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		setCORSHeaders(w)

		if r.Method == http.MethodOptions {
			return
		}

		configPath := "/home/pi/HBQ_server_drone/Find_landing/landing_config.json"
		configData, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "Config file not found",
				})
			} else {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "Failed to read config: " + err.Error(),
				})
			}
			return
		}

		var config map[string]interface{}
		if err := json.Unmarshal(configData, &config); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Failed to parse config: " + err.Error(),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"config":  config,
		})
	})

	// API endpoint to get real-time network info
	http.HandleFunc("/api/network/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		setCORSHeaders(w)

		if r.Method == http.MethodOptions {
			return
		}

		// Read connection status file
		statusFile := "/tmp/connection_status.json"
		statusData, err := os.ReadFile(statusFile)
		
		var networkInfo map[string]interface{}
		
		if err == nil {
			json.Unmarshal(statusData, &networkInfo)
		} else {
		// Return default structure if file doesn't exist
		networkInfo = map[string]interface{}{
			"4g": map[string]interface{}{"status": "unavailable"},
			"wifi": map[string]interface{}{"status": "unavailable"},
			"ethernet": map[string]interface{}{"status": "unavailable"},
			"active_interface": nil,
			"timestamp": time.Now().Unix(),
		}
	}

	// Return network status directly (not wrapped)
	json.NewEncoder(w).Encode(networkInfo)
			return
		}

		if r.Method == http.MethodPost {
			// Set priority
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "Invalid JSON",
				})
				return
			}

			priority := req["priority"]
			if priority != "4g" && priority != "wifi" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "Invalid priority. Use: 4g or wifi",
				})
				return
			}

			// Run connection_manager.py to set priority
			cmd := exec.Command("python3", "/home/pi/connection_manager.py", "priority", priority)
			if err := cmd.Run(); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"message": "Failed to set priority",
				})
				return
			}

			// Trigger reconnect with new priority
			go func() {
				time.Sleep(1 * time.Second)
				exec.Command("python3", "/home/pi/connection_manager.py", "once").Run()
			}()

			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "Priority set to " + priority,
				"priority": priority,
			})
		} else {
			// Get priority
			configData, err := os.ReadFile("/home/pi/connection_config.json")
			var config map[string]interface{}
			
			if err == nil {
				json.Unmarshal(configData, &config)
			} else {
				config = map[string]interface{}{"priority": "4g"}
			}

			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"priority": config["priority"],
			})
		}
	})

	// API endpoint to trigger network reconnection
	http.HandleFunc("/api/network/reconnect", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		setCORSHeaders(w)

		if r.Method == http.MethodOptions {
			return
		}

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Method not allowed",
			})
			return
		}

		// Run connection_manager.py to reconnect
		go func() {
			cmd := exec.Command("python3", "/home/pi/connection_manager.py", "once")
			if err := cmd.Run(); err != nil {
				log.Printf("Failed to trigger reconnection: %v", err)
			}
		}()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Reconnection triggered",
		})
	})

	// Auto-start camera streamer
	go startCameraStreamer()

	// Create HTTP server with optimized settings
	server := &http.Server{
		Addr:           fmt.Sprintf("0.0.0.0:%d", port),
		Handler:        http.DefaultServeMux,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	log.Printf("Starting web server on http://%s", server.Addr)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Web server error: %v", err)
		}
	}()
}

// loadXMLCache loads the PX4 parameter XML file into memory for faster serving
func loadXMLCache() {
	xmlOnce.Do(func() {
		data, err := staticFiles.ReadFile("static/PX4ParameterFactMetaData.xml")
		if err != nil {
			log.Printf("[WEB] Warning: Failed to pre-load XML cache: %v", err)
			xmlContent = []byte{}
		} else {
			xmlContent = data
			log.Printf("[WEB] Pre-loaded PX4ParameterFactMetaData.xml into cache (%d bytes)", len(xmlContent))
		}
	})
}
