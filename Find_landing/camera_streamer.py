
"""
Camera Streamer with Landing Detection Overlay
Streams processed video with detection overlay to MediaMTX server
Uses find.py as detection module and camera_manager for camera access
"""

import cv2
import subprocess
import json
import os
import sys
import time
from threading import Thread, Event
import signal


import find

class CameraStreamer:
    def __init__(self, config_path='camera_config.json'):
        """Initialize camera streamer with configuration"""
        self.config_path = config_path
        self.config = self.load_config()
        self.running = Event()
        self.detection_result = None
        self.gst_process = None
        self.pipe_path = f"/tmp/camera_stream_{os.getpid()}.fifo"
        
        
        self.frames_sent = 0
        self.detections_count = 0
        self.start_time = None
        
        
        self.template_contour = None
        self.template_image = None
        
    def load_config(self):
        """Load configuration from JSON file"""
        default_config = {
            'camera_id': 0,
            'size': [1280, 720],
            'framerate': 30,
            'format': 'RGB888',
            'mediamtx_host': '45.117.171.237',
            'mediamtx_port': 8554,
            'drone_id': 'bé gái đẹp trai',
            'bitrate': 5000,
            'overlay_enabled': True,
            'detection_enabled': True,
            'keyframe_interval': 30,
            'preset': 'ultrafast',
            'tune': 'zerolatency'
        }
        
        if os.path.exists(self.config_path):
            try:
                with open(self.config_path, 'r') as f:
                    loaded_config = json.load(f)
                    default_config.update(loaded_config)
                    print(f"✓ Loaded config from {self.config_path}")
            except Exception as e:
                print(f"⚠ Error loading config: {e}, using defaults")
        
        return default_config
    
    def save_config(self):
        """Save current configuration to JSON file"""
        try:
            with open(self.config_path, 'w') as f:
                json.dump(self.config, f, indent=2)
            print(f"✓ Config saved to {self.config_path}")
        except Exception as e:
            print(f"⚠ Error saving config: {e}")
    
    def build_gstreamer_pipeline(self):
        """Build GStreamer pipeline for RTSP streaming directly from camera"""
        width, height = self.config['size']
        fps = self.config['framerate']
        camera_id = self.config['camera_id']
        
        # Use RTSP for publishing to MediaMTX (tested and working)
        rtsp_url = f"rtsp://{self.config['mediamtx_host']}:{self.config['mediamtx_port']}/{self.config['drone_id']}"
        
        # Direct pipeline from v4l2src (no Python overlay)
        pipeline = (
            f"v4l2src device=/dev/video{camera_id} io-mode=mmap ! "
            f"image/jpeg,width={width},height={height} ! "
            f"jpegdec ! "
            f"videorate ! "
            f"video/x-raw,framerate={fps}/1 ! "
            f"videoconvert ! "
            f"x264enc tune={self.config['tune']} speed-preset={self.config['preset']} "
            f"bitrate={self.config['bitrate']} key-int-max={self.config['keyframe_interval']} ! "
            f"h264parse ! "
            f"rtspclientsink location={rtsp_url}"
        )
        
        print(f"\n📡 GStreamer Pipeline (Direct Camera Stream):")
        print(f"   Camera: /dev/video{camera_id}")
        print(f"   Resolution: {width}x{height} @ {fps}fps")
        print(f"   Bitrate: {self.config['bitrate']} kbps")
        print(f"   RTSP: {rtsp_url}")
        print(f"   WebRTC: http://{self.config['mediamtx_host']}:8889/{self.config['drone_id']}/whep")
        print(f"   HLS: http://{self.config['mediamtx_host']}:8888/{self.config['drone_id']}/index.m3u8")
        print()
        return pipeline
    
    def start_gstreamer(self):
        """Start GStreamer pipeline (direct from camera, no pipe needed)"""
        pipeline = self.build_gstreamer_pipeline()
        
        try:
            # Run GStreamer directly from camera
            self.gst_process = subprocess.Popen(
                f"gst-launch-1.0 {pipeline}",
                shell=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE
            )
            
            time.sleep(2)  
            
            if self.gst_process.poll() is None:
                print("✅ GStreamer pipeline running")
                return True
            else:
                # Get error output
                _, stderr = self.gst_process.communicate(timeout=1)
                print(f"✗ GStreamer exited with code {self.gst_process.returncode}")
                if stderr:
                    print(f"✗ Error: {stderr.decode()}")
                return False
                
        except Exception as e:
            print(f"✗ Failed to start GStreamer: {e}")
            return False
    
    def draw_overlay(self, frame, detection_result):
        """Draw detection overlay on frame - same as find.py local mode"""
        if not self.config.get('overlay_enabled', True) or not detection_result:
            return frame
        
        if not detection_result.get('detected', False):
            
            cv2.putText(frame, "SEARCHING...", (10, 30), 
                       cv2.FONT_HERSHEY_SIMPLEX, 0.7, (0, 255, 255), 2)
            return frame
        
        frame_height, frame_width = frame.shape[:2]
        screen_center_x = frame_width // 2
        screen_center_y = frame_height // 2
        
        
        cv2.line(frame, (screen_center_x - 30, screen_center_y), 
                (screen_center_x + 30, screen_center_y), (255, 0, 0), 2)
        cv2.line(frame, (screen_center_x, screen_center_y - 30), 
                (screen_center_x, screen_center_y + 30), (255, 0, 0), 2)
        
        
        h_x, h_y = detection_result['h_position']
        w, h = detection_result['h_size']
        
        
        cv2.rectangle(frame, (h_x - w//2, h_y - h//2), 
                     (h_x + w//2, h_y + h//2), (0, 255, 0), 3)
        
        
        cv2.line(frame, (h_x - 20, h_y), (h_x + 20, h_y), (0, 0, 255), 3)
        cv2.line(frame, (h_x, h_y - 20), (h_x, h_y + 20), (0, 0, 255), 3)
        cv2.circle(frame, (h_x, h_y), 8, (0, 0, 255), -1)
        
        
        cv2.line(frame, (h_x, h_y), (screen_center_x, screen_center_y), (0, 255, 255), 3)
        
        
        if detection_result.get('in_circle', False) and detection_result.get('circle_center') and detection_result.get('circle_radius'):
            circle_center = detection_result['circle_center']
            circle_radius = detection_result['circle_radius']
            cv2.circle(frame, circle_center, circle_radius, (255, 0, 255), 2)
            cv2.circle(frame, circle_center, 3, (255, 0, 255), -1)
            cv2.putText(frame, "LANDING AREA", (10, 30), 
                       cv2.FONT_HERSHEY_SIMPLEX, 0.7, (255, 0, 255), 2)
        
        
        offset_x = detection_result['offset_x']
        offset_y = detection_result['offset_y']
        direction = detection_result['direction']
        
        cv2.putText(frame, f"Offset: X={offset_x:+4d} Y={offset_y:+4d}", 
                   (10, 60), cv2.FONT_HERSHEY_SIMPLEX, 0.6, (255, 255, 255), 2)
        
        if direction != "CENTER":
            cv2.putText(frame, f"Move: {direction}", 
                       (10, 90), cv2.FONT_HERSHEY_SIMPLEX, 0.7, (0, 255, 255), 2)
        else:
            cv2.putText(frame, "ALIGNED!", 
                       (10, 90), cv2.FONT_HERSHEY_SIMPLEX, 0.7, (0, 255, 0), 2)
        
        
        sim = detection_result.get('similarity', 0)
        cv2.putText(frame, f"Score: {sim:.3f}", 
                   (10, 120), cv2.FONT_HERSHEY_SIMPLEX, 0.6, (255, 255, 255), 2)
        
        return frame
    
    def capture_and_stream_thread(self, pipe_write_fd):
        """Thread for capturing frames, running detection, and streaming"""
        from camera_manager import get_camera_manager
        
        camera_id = self.config['camera_id']
        user_id = "streamer"
        
        cam_manager = get_camera_manager()
        
        
        if cam_manager.is_camera_active(camera_id):
            print("⚠️  Camera already in use, releasing...")
            cam_manager.release_camera(camera_id, user_id)
            time.sleep(1)
        
        camera_config = {
            'format': self.config['format'],
            'size': tuple(self.config['size'])
        }
        
        camera = cam_manager.get_camera(camera_id, user_id, camera_config)
        if camera is None:
            print("✗ Failed to initialize camera")
            self.running.clear()
            return
        
        print("✓ Camera initialized")
        
        # Load detection template
        if self.config['detection_enabled']:
            try:
                # Load landing config to get template setting
                landing_config_path = os.path.join(os.path.dirname(__file__), "landing_config.json")
                template_name = "H"  # Default
                
                if os.path.exists(landing_config_path):
                    try:
                        with open(landing_config_path, 'r') as f:
                            landing_config = json.load(f)
                            template_name = landing_config.get('template', 'H')
                            print(f"✓ Using template: {template_name} from landing config")
                    except Exception as e:
                        print(f"⚠️  Error loading landing config: {e}, using default template H")
                
                # Load template file
                template_path = os.path.join(os.path.dirname(__file__), "templates", f"{template_name}.png")
                if not os.path.exists(template_path):
                    # Fallback to H.png if specified template doesn't exist
                    print(f"⚠️  Template {template_name}.png not found, using H.png")
                    template_path = os.path.join(os.path.dirname(__file__), "templates", "H.png")
                
                self.template_contour, self.template_image = find.load_template(template_path)
                print(f"✓ Detection template loaded: {os.path.basename(template_path)}")
            except Exception as e:
                print(f"⚠️  Detection init failed: {e}")
                self.config['detection_enabled'] = False
        else:
            print("ℹ️  Detection disabled")
        
        fps_interval = 1.0 / self.config['framerate']
        last_frame_time = 0
        frame_count = 0
        last_stats_time = time.time()
        self.start_time = time.time()
        
        
        FRAME_SKIP = 3  
        
        try:
            while self.running.is_set():
                current_time = time.time()
                if current_time - last_frame_time < fps_interval:
                    time.sleep(0.001)
                    continue
                
                last_frame_time = current_time
                
                
                frame = cam_manager.capture_frame(camera_id, user_id)
                if frame is None:
                    continue
                
                frame_count += 1
                
                
                frame_bgr = cv2.cvtColor(frame, cv2.COLOR_RGB2BGR)
                
                
                if self.config['detection_enabled'] and (frame_count % FRAME_SKIP == 0):
                    try:
                        
                        results, _, _ = find.recognize_H(
                            frame_bgr, 
                            self.template_contour,
                            threshold=0.5
                        )
                        
                        if results and len(results) > 0:
                            
                            result = results[0]
                            x, y, w, h = result['bbox']
                            h_x = x + w // 2
                            h_y = y + h // 2
                            h_sim = result['similarity']
                            
                            height, width = frame_bgr.shape[:2]
                            center_x, center_y = width // 2, height // 2
                            offset_x = h_x - center_x
                            offset_y = center_y - h_y  
                            
                            # Check if H is inside a circle (landing pad)
                            circles = find.detect_circles(frame_bgr)
                            in_circle = False
                            circle_center = None
                            circle_radius = None
                            if circles and len(circles) > 0:
                                # Use first detected circle
                                circle = circles[0]
                                circle_center = circle.get('center')
                                
                                # Handle different circle types (ring, ellipse, circle)
                                circle_type = circle.get('type', 'circle')
                                if circle_type == 'ring':
                                    circle_radius = circle.get('radius_outer', 0)
                                elif circle_type == 'ellipse':
                                    axes = circle.get('ellipse_axes', (0, 0))
                                    circle_radius = max(axes) if axes else 0
                                else:  # regular circle
                                    circle_radius = circle.get('radius', 0)
                                
                                if circle_center and circle_radius:
                                    dist_to_circle = ((h_x - circle_center[0])**2 + (h_y - circle_center[1])**2)**0.5
                                    in_circle = dist_to_circle <= circle_radius
                            
                            # Calculate movement direction
                            direction = self.get_direction(offset_x, offset_y)
                            
                            self.detection_result = {
                                'detected': True,
                                'h_position': (h_x, h_y),
                                'h_size': (w, h),
                                'offset_x': offset_x,
                                'offset_y': offset_y,
                                'similarity': h_sim,
                                'in_circle': in_circle,
                                'circle_center': circle_center,
                                'circle_radius': circle_radius,
                                'direction': direction
                            }
                            self.detections_count += 1
                        else:
                            self.detection_result = {'detected': False}
                            
                    except Exception as e:
                        print(f"⚠️  Detection error: {e}")
                
                
                if self.config.get('overlay_enabled', True):
                    if self.detection_result and self.detection_result.get('detected'):
                        frame_bgr = self.draw_overlay(frame_bgr, self.detection_result)
                    elif self.config['detection_enabled']:
                        
                        cv2.putText(frame_bgr, "SEARCHING...", (10, 30), 
                                   cv2.FONT_HERSHEY_SIMPLEX, 0.7, (0, 255, 255), 2)
                
                
                try:
                    os.write(pipe_write_fd, frame_bgr.tobytes())
                    self.frames_sent += 1
                    
                    if frame_count == 1:
                        print(f"✅ First frame streamed!")
                        
                except Exception as e:
                    print(f"✗ Write error: {e}")
                    break
                
                
                if current_time - last_stats_time >= 5.0:
                    elapsed = current_time - self.start_time
                    fps_actual = self.frames_sent / elapsed
                    detection_rate = (self.detections_count / self.frames_sent * 100) if self.frames_sent > 0 else 0
                    
                    print(f"📊 Stats: {self.frames_sent} frames @ {fps_actual:.1f} fps | "
                          f"Detections: {self.detections_count} ({detection_rate:.1f}%)")
                    last_stats_time = current_time
                    
        except Exception as e:
            print(f"✗ Capture error: {e}")
            import traceback
            traceback.print_exc()
        finally:
            cam_manager.release_camera(camera_id, user_id)
            print("✓ Camera released")
    
    def get_direction(self, offset_x, offset_y, threshold=20):
        """Get direction text from offset values"""
        direction = ""
        if abs(offset_x) > threshold:
            direction += "RIGHT " if offset_x > 0 else "LEFT "
        if abs(offset_y) > threshold:
            direction += "DOWN " if offset_y > 0 else "UP "
        if not direction:
            direction = "CENTER"
        return direction.strip()
    
    def start(self):
        """Start streaming directly from camera (no Python overlay)"""
        if not self.running.is_set():
            self.running.set()
        
        print("="*60)
        print("🚀 Starting Camera Streamer (Direct Mode)")
        print("="*60)
        print(f"📹 Camera: /dev/video{self.config['camera_id']}")
        print(f"📐 Resolution: {self.config['size'][0]}x{self.config['size'][1]} @ {self.config['framerate']} fps")
        print(f"📡 Server: {self.config['mediamtx_host']}:{self.config['mediamtx_port']}")
        print(f"🆔 Drone ID: {self.config['drone_id']}")
        print("="*60)
        print("⚠️  Note: Detection overlay disabled in direct mode")
        print("="*60)
        
        # Start GStreamer directly (no pipe needed)
        if not self.start_gstreamer():
            self.running.clear()
            return
        
        print("\n✅ Camera streaming started!")
        print(f"📺 View at:")
        print(f"   - WebRTC: http://{self.config['mediamtx_host']}:8889/{self.config['drone_id']}/whep")
        print(f"   - HLS: http://{self.config['mediamtx_host']}:8888/{self.config['drone_id']}/index.m3u8")
        print(f"   - RTSP: rtsp://{self.config['mediamtx_host']}:{self.config['mediamtx_port']}/{self.config['drone_id']}")
        print("\nPress Ctrl+C to stop...\n")
        
        # Wait for interrupt
        try:
            while self.running.is_set() and self.gst_process.poll() is None:
                time.sleep(1)
            
            if self.gst_process.poll() is not None:
                print(f"\n⚠️  GStreamer stopped unexpectedly (exit code: {self.gst_process.returncode})")
                self.running.clear()
                
        except KeyboardInterrupt:
            print("\n🛑 Stopping camera streamer...")
            self.stop()
    
    
    def stop(self):
        """Stop camera streaming"""
        print("\n🛑 Stopping camera streamer...")
        self.running.clear()
        
        # Stop GStreamer
        if self.gst_process:
            try:
                self.gst_process.terminate()
                self.gst_process.wait(timeout=5)
                print("✓ GStreamer stopped")
            except:
                self.gst_process.kill()
                print("✓ GStreamer killed")
        
        print("✓ Camera streamer stopped")


def signal_handler(sig, frame):
    """Handle Ctrl+C gracefully"""
    print("\n⚠ Signal received, stopping...")
    sys.exit(0)


def main():
    """Main entry point"""
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)
    
    
    config_path = sys.argv[1] if len(sys.argv) > 1 else 'camera_config.json'
    
    streamer = CameraStreamer(config_path)
    streamer.start()


if __name__ == '__main__':
    main()

