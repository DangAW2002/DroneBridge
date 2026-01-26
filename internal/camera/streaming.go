package camera

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"DroneBridge/internal/logger"
)

// StreamingConfig holds camera streaming configuration
type StreamingConfig struct {
	CameraID         int    `json:"camera_id"`
	Size             []int  `json:"size"`      // [width, height]
	Framerate        int    `json:"framerate"` // fps
	Format           string `json:"format"`    // RGB888, etc
	MediaMTXHost     string `json:"mediamtx_host"`
	MediaMTXPort     int    `json:"mediamtx_port"`
	DroneID          string `json:"drone_id"`
	Bitrate          int    `json:"bitrate"` // kbps
	OverlayEnabled   bool   `json:"overlay_enabled"`
	DetectionEnabled bool   `json:"detection_enabled"`
	KeyframeInterval int    `json:"keyframe_interval"`
	Preset           string `json:"preset"` // ultrafast, superfast, veryfast
	Tune             string `json:"tune"`   // zerolatency
	Enabled          bool   `json:"enabled"`
}

// LoadConfig loads configuration from JSON file
func LoadConfig(configPath string) (*StreamingConfig, error) {
	cfg := &StreamingConfig{
		CameraID:         0,
		Size:             []int{1280, 720},
		Framerate:        30,
		Format:           "RGB888",
		MediaMTXHost:     "45.117.171.237",
		MediaMTXPort:     8554,
		DroneID:          "test_mycamera",
		Bitrate:          5000,
		OverlayEnabled:   true,
		DetectionEnabled: true,
		KeyframeInterval: 30,
		Preset:           "ultrafast",
		Tune:             "zerolatency",
		Enabled:          false, // Disabled - Python handles streaming
	}

	// Load from file if exists
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err == nil {
			if err := json.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("failed to parse config: %w", err)
			}
		}
	}

	return cfg, nil
}

// SaveConfig saves configuration to JSON file
func (c *StreamingConfig) SaveConfig(configPath string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// Streamer manages H.264 video streaming via GStreamer
type Streamer struct {
	config   *StreamingConfig
	cmd      *exec.Cmd
	running  bool
	mu       sync.Mutex
	authHost string
	uuid     string
}

// NewStreamer creates a new streamer instance
func NewStreamer(cfg *StreamingConfig, authHost, uuid string) *Streamer {
	return &Streamer{
		config:   cfg,
		running:  false,
		authHost: authHost,
		uuid:     uuid,
	}
}

// Start begins the H.264 video streaming
func (s *Streamer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("streaming already running")
	}

	if !s.config.Enabled {
		logger.Info("[STREAMING] Video streaming disabled")
		return nil
	}

	logger.Info("[STREAMING] Starting H.264 stream (device=%d, resolution=%dx%d, bitrate=%d kbps)",
		s.config.CameraID, s.config.Size[0], s.config.Size[1], s.config.Bitrate)

	// Build GStreamer pipeline
	pipeline := s.buildPipeline()
	if pipeline == "" {
		return fmt.Errorf("unsupported platform")
	}

	// Start GStreamer
	args := strings.Split(pipeline, " ")
	s.cmd = exec.Command("gst-launch-1.0", args...)

	// Redirect GStreamer output to log file instead of stdout/stderr
	logFile, err := os.OpenFile("logs/gstreamer.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		logger.Warn("[STREAMING] Failed to open GStreamer log file: %v, using stdout", err)
		s.cmd.Stdout = os.Stdout
		s.cmd.Stderr = os.Stderr
	} else {
		s.cmd.Stdout = logFile
		s.cmd.Stderr = logFile
	}

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start GStreamer: %w", err)
	}

	s.running = true
	logger.Info("[STREAMING] ✅ H.264 streaming started (PID: %d)", s.cmd.Process.Pid)

	// Monitor process in background
	go func() {
		err := s.cmd.Wait()
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()

		if err != nil {
			logger.Warn("[STREAMING] GStreamer exited with error: %v", err)
		} else {
			logger.Info("[STREAMING] GStreamer stopped")
		}
	}()

	// Wait for pipeline to stabilize
	time.Sleep(2 * time.Second)

	return nil
}

// buildPipeline constructs the GStreamer pipeline based on platform
func (s *Streamer) buildPipeline() string {
	width := s.config.Size[0]
	height := s.config.Size[1]
	fps := s.config.Framerate
	cameraID := s.config.CameraID
	bitrate := s.config.Bitrate
	preset := s.config.Preset
	tune := s.config.Tune
	keyframe := s.config.KeyframeInterval

	// Build RTSP URL
	rtspURL := fmt.Sprintf("rtsp://%s:%d/%s",
		s.config.MediaMTXHost,
		s.config.MediaMTXPort,
		url.QueryEscape(s.uuid))

	logger.Info("[STREAMING] RTSP URL: %s", rtspURL)

	osName := runtime.GOOS
	var pipeline string

	switch osName {
	case "windows":
		// Windows: Use Media Foundation video source
		pipeline = fmt.Sprintf(
			"mfvideosrc device-index=%d ! "+
				"video/x-raw,width=%d,height=%d,framerate=%d/1 ! "+
				"videoconvert ! "+
				"video/x-raw,format=I420 ! "+
				"x264enc tune=%s speed-preset=%s bitrate=%d key-int-max=%d ! "+
				"h264parse ! "+
				"rtspclientsink location=%s",
			cameraID, width, height, fps,
			tune, preset, bitrate, keyframe,
			rtspURL)

	case "linux":
		// Linux: Use Video4Linux2 source
		pipeline = fmt.Sprintf(
			"v4l2src device=/dev/video%d io-mode=mmap ! "+
				"image/jpeg,width=%d,height=%d ! "+
				"jpegdec ! "+
				"videorate ! "+
				"video/x-raw,framerate=%d/1 ! "+
				"videoconvert ! "+
				"x264enc tune=%s speed-preset=%s bitrate=%d key-int-max=%d ! "+
				"h264parse ! "+
				"rtspclientsink location=%s",
			cameraID, width, height, fps,
			tune, preset, bitrate, keyframe,
			rtspURL)

	case "darwin":
		// macOS: Use AVFoundation source
		pipeline = fmt.Sprintf(
			"avfvideosrc ! "+
				"video/x-raw,width=%d,height=%d,framerate=%d/1 ! "+
				"videoconvert ! "+
				"video/x-raw,format=I420 ! "+
				"x264enc tune=%s speed-preset=%s bitrate=%d key-int-max=%d ! "+
				"h264parse ! "+
				"rtspclientsink location=%s",
			width, height, fps,
			tune, preset, bitrate, keyframe,
			rtspURL)

	default:
		logger.Warn("[STREAMING] Unsupported platform: %s", osName)
		return ""
	}

	logger.Info("[STREAMING] Pipeline: %s", pipeline)
	return pipeline
}

// Stop stops the video streaming
func (s *Streamer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.cmd == nil || s.cmd.Process == nil {
		return nil
	}

	logger.Info("[STREAMING] Stopping H.264 streaming...")

	if err := s.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to stop streaming: %w", err)
	}

	s.running = false
	logger.Info("[STREAMING] ✅ H.264 streaming stopped")
	return nil
}

// IsRunning returns whether streaming is active
func (s *Streamer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// StartH264Streaming is a convenience function for simple usage
func StartH264Streaming(cfg *StreamingConfig, authHost string, uuid string) {
	if !cfg.Enabled {
		log.Println("📹 Video streaming disabled")
		return
	}

	streamer := NewStreamer(cfg, authHost, uuid)
	if err := streamer.Start(); err != nil {
		log.Printf("❌ Failed to start streaming: %v", err)
		return
	}
}
