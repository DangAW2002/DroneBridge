package camera

import (
	"fmt"
	"sync"
	"time"

	"DroneBridge/internal/logger"
)

// Camera represents a camera device
type Camera struct {
	ID       int
	Name     string
	Config   *StreamingConfig
	Streamer *Streamer
	mu       sync.RWMutex
}

// Manager manages multiple cameras
type Manager struct {
	cameras map[int]*Camera
	mu      sync.RWMutex
}

// NewManager creates a new camera manager
func NewManager() *Manager {
	return &Manager{
		cameras: make(map[int]*Camera),
	}
}

// LoadCamera loads and initializes a camera
func (m *Manager) LoadCamera(configPath string, authHost, uuid string) (*Camera, error) {
	// Load configuration
	cfg, err := LoadConfig(configPath)
	if err != nil && configPath != "" {
		logger.Warn("[CAMERA] Failed to load config from %s: %v, using defaults", configPath, err)
		cfg, _ = LoadConfig("")
	}

	if cfg == nil {
		cfg, _ = LoadConfig("")
	}

	return m.LoadCameraFromConfig(cfg, authHost, uuid)
}

// LoadCameraFromConfig loads camera with StreamingConfig
func (m *Manager) LoadCameraFromConfig(cfg *StreamingConfig, authHost, uuid string) (*Camera, error) {
	if cfg == nil {
		return nil, fmt.Errorf("camera config is nil")
	}

	cameraID := cfg.CameraID

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if camera already exists
	if _, exists := m.cameras[cameraID]; exists {
		return nil, fmt.Errorf("camera %d already loaded", cameraID)
	}

	// Create camera
	camera := &Camera{
		ID:     cameraID,
		Name:   fmt.Sprintf("Camera %d", cameraID),
		Config: cfg,
	}

	// Create streamer
	camera.Streamer = NewStreamer(cfg, authHost, uuid)

	m.cameras[cameraID] = camera
	logger.Info("[CAMERA] ✅ Camera %d loaded (resolution: %dx%d, fps: %d)",
		cameraID, cfg.Size[0], cfg.Size[1], cfg.Framerate)

	return camera, nil
}

// StartCamera starts a camera stream
func (m *Manager) StartCamera(cameraID int) error {
	m.mu.RLock()
	camera, exists := m.cameras[cameraID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("camera %d not found", cameraID)
	}

	camera.mu.Lock()
	defer camera.mu.Unlock()

	if camera.Streamer == nil {
		return fmt.Errorf("camera %d streamer not initialized", cameraID)
	}

	logger.Info("[CAMERA] Starting camera %d...", cameraID)
	if err := camera.Streamer.Start(); err != nil {
		logger.Error("[CAMERA] Failed to start camera %d: %v", cameraID, err)
		return err
	}

	logger.Info("[CAMERA] ✅ Camera %d started", cameraID)
	return nil
}

// StopCamera stops a camera stream
func (m *Manager) StopCamera(cameraID int) error {
	m.mu.RLock()
	camera, exists := m.cameras[cameraID]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("camera %d not found", cameraID)
	}

	camera.mu.Lock()
	defer camera.mu.Unlock()

	if camera.Streamer == nil {
		return nil
	}

	logger.Info("[CAMERA] Stopping camera %d...", cameraID)
	if err := camera.Streamer.Stop(); err != nil {
		logger.Error("[CAMERA] Failed to stop camera %d: %v", cameraID, err)
		return err
	}

	logger.Info("[CAMERA] ✅ Camera %d stopped", cameraID)
	return nil
}

// StopAll stops all cameras
func (m *Manager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errors []error
	for cameraID, camera := range m.cameras {
		if err := camera.Streamer.Stop(); err != nil {
			logger.Warn("[CAMERA] Error stopping camera %d: %v", cameraID, err)
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors stopping cameras: %v", errors)
	}

	return nil
}

// GetCamera returns a camera by ID
func (m *Manager) GetCamera(cameraID int) (*Camera, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	camera, exists := m.cameras[cameraID]
	if !exists {
		return nil, fmt.Errorf("camera %d not found", cameraID)
	}

	return camera, nil
}

// GetAllCameras returns all loaded cameras
func (m *Manager) GetAllCameras() []*Camera {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cameras := make([]*Camera, 0, len(m.cameras))
	for _, camera := range m.cameras {
		cameras = append(cameras, camera)
	}

	return cameras
}

// IsRunning checks if a camera is running
func (c *Camera) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.Streamer == nil {
		return false
	}

	return c.Streamer.IsRunning()
}

// UpdateConfig updates camera configuration
func (c *Camera) UpdateConfig(newConfig *StreamingConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Streamer.IsRunning() {
		return fmt.Errorf("cannot update config while streaming")
	}

	c.Config = newConfig
	logger.Info("[CAMERA] ✅ Camera %d config updated", c.ID)

	return nil
}

// SaveConfig saves camera configuration to file
func (c *Camera) SaveConfig(configPath string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if err := c.Config.SaveConfig(configPath); err != nil {
		logger.Error("[CAMERA] Failed to save config: %v", err)
		return err
	}

	logger.Info("[CAMERA] ✅ Camera %d config saved to %s", c.ID, configPath)
	return nil
}

// Config for global camera manager instance
var globalManager *Manager
var managerOnce sync.Once

// GetManager returns the global camera manager instance
func GetManager() *Manager {
	managerOnce.Do(func() {
		globalManager = NewManager()
	})
	return globalManager
}

// InitializeFromConfig initializes cameras from configuration
func InitializeFromConfig(cfgCamera interface{}, authHost, uuid string) error {
	mgr := GetManager()

	// Convert from different config types
	var cfg *StreamingConfig

	switch v := cfgCamera.(type) {
	case *StreamingConfig:
		// Already a StreamingConfig
		cfg = v
	default:
		// Create default config using UUID
		cfg = &StreamingConfig{
			CameraID:         0,
			Size:             []int{1280, 720},
			Framerate:        30,
			Format:           "RGB888",
			MediaMTXHost:     "45.117.171.237",
			MediaMTXPort:     8554,
			DroneID:          uuid, // Use auth UUID as drone ID
			Bitrate:          5000,
			OverlayEnabled:   true,
			DetectionEnabled: false,
			KeyframeInterval: 30,
			Preset:           "ultrafast",
			Tune:             "zerolatency",
			Enabled:          true,
		}
	}

	_, err := mgr.LoadCameraFromConfig(cfg, authHost, uuid)
	return err
}

// StartAllCameras starts all loaded cameras
func StartAllCameras() error {
	mgr := GetManager()
	cameras := mgr.GetAllCameras()

	if len(cameras) == 0 {
		logger.Warn("[CAMERA] No cameras loaded")
		return nil
	}

	for _, camera := range cameras {
		if err := mgr.StartCamera(camera.ID); err != nil {
			logger.Error("[CAMERA] Failed to start camera %d: %v", camera.ID, err)
		}
	}

	return nil
}

// WaitForCameras waits for all cameras to be ready
func WaitForCameras(timeout time.Duration) error {
	mgr := GetManager()
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for cameras")
		}

		allRunning := true
		for _, camera := range mgr.GetAllCameras() {
			if !camera.IsRunning() {
				allRunning = false
				break
			}
		}

		if allRunning && len(mgr.GetAllCameras()) > 0 {
			return nil
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// GracefulShutdown stops all cameras gracefully
func GracefulShutdown() {
	mgr := GetManager()
	logger.Info("[CAMERA] Initiating graceful shutdown...")

	mgr.mu.Lock()
	for cameraID := range mgr.cameras {
		mgr.mu.Unlock()

		if err := mgr.StopCamera(cameraID); err != nil {
			logger.Warn("[CAMERA] Error stopping camera %d: %v", cameraID, err)
		}

		mgr.mu.Lock()
	}
	mgr.mu.Unlock()

	logger.Info("[CAMERA] ✅ All cameras stopped")
}

// Cleanup releases all camera resources
func Cleanup() {
	mgr := GetManager()
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	for _, camera := range mgr.cameras {
		if camera.Streamer != nil {
			camera.Streamer.Stop()
		}
	}

	mgr.cameras = make(map[int]*Camera)
	logger.Info("[CAMERA] ✅ All resources cleaned up")
}
