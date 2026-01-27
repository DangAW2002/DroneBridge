import threading
import time
from picamera2 import Picamera2
import numpy as np


class CameraManager:
  
    _instance = None
    _lock = threading.Lock()
    
    def __new__(cls):
        if cls._instance is None:
            with cls._lock:
                if cls._instance is None:
                    cls._instance = super(CameraManager, cls).__new__(cls)
                    cls._instance._initialized = False
        return cls._instance
    
    def __init__(self):
        if self._initialized:
            return
            
        with self._lock:
            if not self._initialized:
                self.cameras = {} 
                self.camera_locks = {}  
                self.camera_configs = {}  
                self.camera_users = {}  
                self._initialized = True
    
    def get_camera(self, camera_id, user_id=None, config=None):
      
        with self._lock:
            if camera_id not in self.cameras:
                try:
                    camera = self._initialize_camera(camera_id, config)
                    if camera is None:
                        return None
                    
                    self.cameras[camera_id] = camera
                    self.camera_locks[camera_id] = threading.Lock()
                    self.camera_configs[camera_id] = config or {}
                    self.camera_users[camera_id] = set()
                    
                    print(f"Camera {camera_id} đã được khởi tạo")
                except Exception as e:
                    print(f"Lỗi khởi tạo camera {camera_id}: {e}")
                    return None
            
            if user_id:
                self.camera_users[camera_id].add(user_id)
            
            return self.cameras[camera_id]
    
    def _initialize_camera(self, camera_id, config=None):

        try:
            camera = Picamera2(camera_id)
            
            
            default_config = {
                'format': 'RGB888',
                'size': (640, 480)
            }
            
            if config:
                default_config.update(config)
            
            camera_config = camera.create_still_configuration(
                main={"format": default_config['format'], 
                      "size": default_config['size']}
            )
            
            camera.configure(camera_config)
            camera.start()
            
            time.sleep(0.5)
            
            return camera
            
        except Exception as e:
            print(f"Lỗi trong _initialize_camera cho camera {camera_id}: {e}")
            return None
    
    def capture_frame(self, camera_id, user_id=None):

        camera = self.get_camera(camera_id, user_id)
        if camera is None:
            return None
        
        lock = self.camera_locks.get(camera_id)
        if lock:
            with lock:
                try:
                    frame = camera.capture_array()
                    return frame
                except Exception as e:
                    print(f"Lỗi capture frame từ camera {camera_id}: {e}")
                    return None
        return None
    
    def release_camera(self, camera_id, user_id=None):
      
        with self._lock:
            if camera_id not in self.cameras:
                return
            
            if user_id and camera_id in self.camera_users:
                self.camera_users[camera_id].discard(user_id)
            
            if not self.camera_users.get(camera_id):
                try:
                    camera = self.cameras[camera_id]
                    if camera:
                        camera.stop()
                        camera.close()
                    
                    del self.cameras[camera_id]
                    del self.camera_locks[camera_id]
                    del self.camera_configs[camera_id]
                    del self.camera_users[camera_id]
                    
                    print(f"Camera {camera_id} đã được giải phóng")
                except Exception as e:
                    print(f"Lỗi giải phóng camera {camera_id}: {e}")
    
    def is_camera_active(self, camera_id):
      
        return camera_id in self.cameras
    
    def get_camera_users(self, camera_id):
      
        return self.camera_users.get(camera_id, set()).copy()
    
    def release_all_cameras(self):
       
        with self._lock:
            camera_ids = list(self.cameras.keys())
            for camera_id in camera_ids:
                try:
                    camera = self.cameras[camera_id]
                    if camera:
                        camera.stop()
                        camera.close()
                    print(f"Camera {camera_id} đã được dừng")
                except Exception as e:
                    print(f"Lỗi khi dừng camera {camera_id}: {e}")
            
            self.cameras.clear()
            self.camera_locks.clear()
            self.camera_configs.clear()
            self.camera_users.clear()



_camera_manager_instance = CameraManager()


def get_camera_manager():

    return _camera_manager_instance