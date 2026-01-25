package camera

import (
	"fmt"
	"os/exec"
	"sync"
	"time"

	"DroneBridge/logger"
)

// Config holds camera configuration
type Config struct {
	Enabled    bool
	ConfigFile string
	AutoStart  bool
	StreamPath string
}

// Manager manages camera streaming
type Manager struct {
	config  Config
	cmd     *exec.Cmd
	running bool
	mu      sync.Mutex
}

// NewManager creates a new camera manager
func NewManager(cfg Config) (*Manager, error) {
	return &Manager{
		config:  cfg,
		running: false,
	}, nil
}

// Start starts the camera streaming
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("camera already running")
	}

	logger.Info("[CAMERA] Starting camera streamer...")

	// Start Python camera streamer
	m.cmd = exec.Command("python3", "camera_streamer.py")
	m.cmd.Dir = "Find_landing"

	// Start the process
	if err := m.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start camera streamer: %w", err)
	}

	m.running = true
	logger.Info("[CAMERA] ✅ Camera streamer started (PID: %d)", m.cmd.Process.Pid)

	// Monitor the process in background
	go func() {
		err := m.cmd.Wait()
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()

		if err != nil {
			logger.Warn("[CAMERA] Camera streamer exited with error: %v", err)
		} else {
			logger.Info("[CAMERA] Camera streamer stopped")
		}
	}()

	// Wait a bit to ensure it started properly
	time.Sleep(2 * time.Second)

	return nil
}

// Stop stops the camera streaming
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running || m.cmd == nil || m.cmd.Process == nil {
		return nil
	}

	logger.Info("[CAMERA] Stopping camera streamer...")

	if err := m.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to stop camera: %w", err)
	}

	m.running = false
	logger.Info("[CAMERA] ✅ Camera streamer stopped")
	return nil
}

// IsRunning returns whether camera is running
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}
