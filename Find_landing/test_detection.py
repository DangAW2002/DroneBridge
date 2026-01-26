# -*- coding: utf-8 -*-
"""Test detection on live camera"""
import cv2
import find
import time
from camera_manager import get_camera_manager

def test_detection():
    """Test detection with live camera display"""
    print("[TEST] Starting detection test...")
    print("[TEST] Press 'q' to quit, 's' to save result")
    
    # Initialize camera
    cam_manager = get_camera_manager()
    camera_config = {'format': 'RGB888', 'size': (640, 480)}
    camera = cam_manager.get_camera(0, "test_user", camera_config)
    
    if camera is None:
        print("[ERROR] Failed to init camera")
        return
    
    # Load template
    try:
        template_contour, template_image = find.load_template("./templates/H.png")
        print("[OK] Template loaded")
    except Exception as e:
        print(f"[ERROR] Failed to load template: {e}")
        return
    
    # Capture and detect
    frame_count = 0
    detected_count = 0
    
    print("[INFO] Capturing frames... Press 'q' to quit")
    print("-" * 60)
    
    while True:
        try:
            # Capture frame
            ret, frame = camera.read()
            if not ret:
                print("[ERROR] Failed to read frame")
                break
            
            # BGR to RGB
            frame = cv2.cvtColor(frame, cv2.COLOR_BGR2RGB)
            frame_count += 1
            
            # DETECT CIRCLES
            circles = find.detect_circles(frame)
            
            # RECOGNIZE H
            h_results, _, _ = find.recognize_H(frame, template_contour, threshold=0.8)
            
            # Print results
            if circles or h_results:
                detected_count += 1
                print(f"\n[FRAME {frame_count}] Detection found!")
                print(f"  Circles: {len(circles)}")
                for i, c in enumerate(circles):
                    print(f"    - Circle {i}: center={c['center']}, radius={c['radius']}")
                print(f"  H patterns: {len(h_results)}")
                for i, h in enumerate(h_results):
                    print(f"    - H {i}: {h}")
            else:
                if frame_count % 30 == 0:
                    print(f"[FRAME {frame_count}] No detection (normal)")
            
            # Display
            frame_bgr = cv2.cvtColor(frame, cv2.COLOR_RGB2BGR)
            
            # Draw circles
            for circle in circles:
                cx, cy = circle['center']
                r = circle['radius']
                cv2.circle(frame_bgr, (cx, cy), r, (0, 255, 0), 2)
                cv2.circle(frame_bgr, (cx, cy), 3, (0, 255, 0), -1)
            
            # Draw H results
            for h_res in h_results:
                x, y, w, h = h_res['bbox']
                cv2.rectangle(frame_bgr, (x, y), (x+w, y+h), (0, 0, 255), 2)
                cv2.putText(frame_bgr, f"H: {h_res['similarity']:.3f}", 
                           (x, y-10), cv2.FONT_HERSHEY_SIMPLEX, 0.5, (0, 0, 255), 1)
            
            # Show stats
            cv2.putText(frame_bgr, f"Frame: {frame_count} | Detected: {detected_count}", 
                       (10, 30), cv2.FONT_HERSHEY_SIMPLEX, 0.7, (255, 255, 255), 2)
            
            cv2.imshow("Detection Test", frame_bgr)
            
            # Key control
            key = cv2.waitKey(1) & 0xFF
            if key == ord('q'):
                print("\n[QUIT] User quit")
                break
            elif key == ord('s'):
                cv2.imwrite(f"detection_result_{frame_count}.png", frame_bgr)
                print(f"[SAVE] Saved detection_result_{frame_count}.png")
            
            time.sleep(0.03)  # ~30 FPS
            
        except KeyboardInterrupt:
            print("\n[INTERRUPT] User interrupted")
            break
        except Exception as e:
            print(f"[ERROR] {e}")
            break
    
    print(f"\n[STATS] Total frames: {frame_count}")
    print(f"[STATS] Detections: {detected_count}")
    print(f"[STATS] Detection rate: {detected_count/frame_count*100:.1f}%")
    
    cv2.destroyAllWindows()
    cam_manager.release_camera(0, "test_user")
    print("[OK] Test completed")

if __name__ == "__main__":
    test_detection()
