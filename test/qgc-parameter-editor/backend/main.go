package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
	"github.com/rs/cors"
)

// MAVLink parameter types (matching PX4)
const (
	MAV_PARAM_TYPE_UINT8  = 1
	MAV_PARAM_TYPE_INT8   = 2
	MAV_PARAM_TYPE_UINT16 = 3
	MAV_PARAM_TYPE_INT16  = 4
	MAV_PARAM_TYPE_UINT32 = 5
	MAV_PARAM_TYPE_INT32  = 6
	MAV_PARAM_TYPE_UINT64 = 7
	MAV_PARAM_TYPE_INT64  = 8
	MAV_PARAM_TYPE_REAL32 = 9
	MAV_PARAM_TYPE_REAL64 = 10
)

// ParamSetRequest represents a request to set a parameter
type ParamSetRequest struct {
	ParamName  string  `json:"paramName"`
	ParamValue float64 `json:"paramValue"`
	ParamType  string  `json:"paramType"` // "INT32", "FLOAT", "UINT8", etc.
}

// ParamSetResponse represents the response from setting a parameter
type ParamSetResponse struct {
	Success   bool    `json:"success"`
	Message   string  `json:"message"`
	ParamName string  `json:"paramName"`
	NewValue  float64 `json:"newValue"`
}

// ConnectionStatus represents the current connection state
type ConnectionStatus struct {
	Connected bool   `json:"connected"`
	SystemID  uint8  `json:"systemId"`
	Message   string `json:"message"`
}

// MAVLinkBridge handles MAVLink communication
type MAVLinkBridge struct {
	node           *gomavlib.Node
	pixhawkSysID   byte
	connected      bool
	mutex          sync.RWMutex
	targetAddr     string
	listenAddr     string
	responseTimeout time.Duration
}

var bridge *MAVLinkBridge

func NewMAVLinkBridge(targetAddr, listenAddr string, timeout time.Duration) (*MAVLinkBridge, error) {
	node, err := gomavlib.NewNode(gomavlib.NodeConf{
		Endpoints: []gomavlib.EndpointConf{
			gomavlib.EndpointUDPServer{Address: listenAddr},
			gomavlib.EndpointUDPClient{Address: targetAddr},
		},
		Dialect:     common.Dialect,
		OutVersion:  gomavlib.V2,
		OutSystemID: 255, // GCS system ID
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MAVLink node: %w", err)
	}

	b := &MAVLinkBridge{
		node:           node,
		targetAddr:     targetAddr,
		listenAddr:     listenAddr,
		responseTimeout: timeout,
	}

	// Start heartbeat listener in background
	go b.listenForHeartbeat()

	return b, nil
}

func (b *MAVLinkBridge) listenForHeartbeat() {
	for event := range b.node.Events() {
		if frame, ok := event.(*gomavlib.EventFrame); ok {
			if _, ok := frame.Message().(*common.MessageHeartbeat); ok {
				b.mutex.Lock()
				if !b.connected {
					b.pixhawkSysID = frame.SystemID()
					b.connected = true
					log.Printf("✓ Connected to Pixhawk (System ID: %d)", b.pixhawkSysID)
				}
				b.mutex.Unlock()
			}
		}
	}
}

func (b *MAVLinkBridge) IsConnected() bool {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.connected
}

func (b *MAVLinkBridge) GetSystemID() byte {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.pixhawkSysID
}

func (b *MAVLinkBridge) SetParameter(paramName string, paramValue float64, paramType string) (*ParamSetResponse, error) {
	b.mutex.RLock()
	connected := b.connected
	sysID := b.pixhawkSysID
	b.mutex.RUnlock()

	if !connected {
		return &ParamSetResponse{
			Success:   false,
			Message:   "Not connected to Pixhawk",
			ParamName: paramName,
		}, nil
	}

	// Convert param type string to MAVLink type
	mavParamType := getMavParamType(paramType)

	// Encode the value based on type
	var encodedValue float32
	if mavParamType == common.MAV_PARAM_TYPE_INT32 || mavParamType == common.MAV_PARAM_TYPE_UINT32 ||
		mavParamType == common.MAV_PARAM_TYPE_INT16 || mavParamType == common.MAV_PARAM_TYPE_UINT16 ||
		mavParamType == common.MAV_PARAM_TYPE_INT8 || mavParamType == common.MAV_PARAM_TYPE_UINT8 {
		// PX4 uses bytewise encoding for integer parameters
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

	log.Printf("Sending PARAM_SET: %s = %v (type: %s)", paramName, paramValue, paramType)

	err := b.node.WriteMessageAll(paramMsg)
	if err != nil {
		return &ParamSetResponse{
			Success:   false,
			Message:   fmt.Sprintf("Failed to send PARAM_SET: %v", err),
			ParamName: paramName,
		}, nil
	}

	// Wait for PARAM_VALUE response
	response := b.waitForParamResponse(paramName)
	return response, nil
}

func (b *MAVLinkBridge) waitForParamResponse(paramName string) *ParamSetResponse {
	timeout := time.After(b.responseTimeout)
	eventCh := b.node.Events()

	for {
		select {
		case event := <-eventCh:
			if frame, ok := event.(*gomavlib.EventFrame); ok {
				if msg, ok := frame.Message().(*common.MessageParamValue); ok {
					if msg.ParamId == paramName {
						// Decode value based on type
						var decodedValue float64
						if msg.ParamType == common.MAV_PARAM_TYPE_INT32 {
							decodedValue = float64(int32(math.Float32bits(msg.ParamValue)))
						} else {
							decodedValue = float64(msg.ParamValue)
						}

						log.Printf("✓ PARAM_VALUE received: %s = %v", paramName, decodedValue)

						return &ParamSetResponse{
							Success:   true,
							Message:   fmt.Sprintf("Parameter %s successfully set", paramName),
							ParamName: paramName,
							NewValue:  decodedValue,
						}
					}
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
	case "FLOAT", "float":
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

// HTTP Handlers
func handleSetParameter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ParamSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	log.Printf("Received param set request: %+v", req)

	response, err := bridge.SetParameter(req.ParamName, req.ParamValue, req.ParamType)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleConnectionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := ConnectionStatus{
		Connected: bridge.IsConnected(),
		SystemID:  bridge.GetSystemID(),
	}

	if status.Connected {
		status.Message = fmt.Sprintf("Connected to Pixhawk (System ID: %d)", status.SystemID)
	} else {
		status.Message = "Waiting for Pixhawk connection..."
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func main() {
	// Command line flags
	targetAddr := flag.String("target", "10.41.10.2:14550", "Target Pixhawk address")
	listenAddr := flag.String("listen", ":14550", "Address to listen for MAVLink messages")
	httpPort := flag.String("port", "8080", "HTTP server port")
	timeout := flag.Duration("timeout", 5*time.Second, "Timeout for waiting parameter response")
	flag.Parse()

	log.Printf("Starting MAVLink Parameter Bridge...")
	log.Printf("  - Target Pixhawk: %s", *targetAddr)
	log.Printf("  - Listen address: %s", *listenAddr)
	log.Printf("  - HTTP port: %s", *httpPort)

	var err error
	bridge, err = NewMAVLinkBridge(*targetAddr, *listenAddr, *timeout)
	if err != nil {
		log.Fatalf("Failed to create MAVLink bridge: %v", err)
	}

	// Setup HTTP routes with CORS
	mux := http.NewServeMux()
	mux.HandleFunc("/api/param/set", handleSetParameter)
	mux.HandleFunc("/api/connection", handleConnectionStatus)
	mux.HandleFunc("/api/health", handleHealth)

	// Enable CORS for frontend
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "http://localhost:3000", "*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	handler := c.Handler(mux)

	log.Printf("✓ HTTP server starting on :%s", *httpPort)
	log.Fatal(http.ListenAndServe(":"+*httpPort, handler))
}
