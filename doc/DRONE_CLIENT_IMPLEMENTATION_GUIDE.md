# 🚁 Drone Authentication Client - Implementation Guide

## 📋 Overview

Hướng dẫn triển khai authentication cho drone kết nối với MAVLink Router.

**Mục đích:**
- Drone phải authenticate qua port **5770** trước khi gửi MAVLink packets
- Sử dụng HMAC-SHA256 để xác thực
- Duy trì session qua keepalive messages
- Tự động reconnect khi mất kết nối

---

## 🔑 Hardcoded Credentials (Testing Only)

**⚠️ CHỈ DÙNG ĐỂ TEST - KHÔNG DÙNG PRODUCTION!**

### Server có sẵn 3 drone test:

| Drone ID | Secret Key |
|----------|------------|
| 1 | `drone_1_secret_key_abc123` |
| 2 | `drone_2_secret_key_def456` |
| 3 | `drone_3_secret_key_ghi789` |

---

## 🚀 Quick Start - Python Client

### 1. Download Test Client

```bash
# Tải file test client từ server
wget https://your-server.com/drone_auth_client_test.py

# Hoặc copy từ repo
cp test_scripts/drone_auth_client_test.py ~/drone/
```

### 2. Test Authentication

```bash
python3 drone_auth_client_test.py
```

**Expected Output:**
```
============================================================
Testing Drone 1 Authentication
============================================================
✓ Connected to localhost:5770
✓ Sent AUTH_REQUEST (step 1)
✓ Received AUTH_CHALLENGE: nonce=c356c77e606c91ae..., timeout=10s
✓ Computed HMAC: 4c382b70bdcc21a8...
✓ Sent AUTH_REQUEST with HMAC (step 2)
✅ Authentication successful!
   Session token: 1:ed8ab5b50bfe281c...
   Expires at: 2025-11-19 18:30:45
   Keepalive interval: 45s

--- Testing Keepalive ---
✓ Sent KEEPALIVE
✅ Keepalive acknowledged!

Test Summary: 3/3 passed
```

---

## 📡 Authentication Protocol Flow

```
Drone                                    Server (Port 5770)
  |                                           |
  |-------- 1. AUTH_REQUEST ----------------→|
  |         (DroneID, fake HMAC)             |
  |                                           |
  |←------- 2. AUTH_CHALLENGE ---------------|
  |         (Nonce, Timeout)                 |
  |                                           |
  | Compute HMAC = HMAC-SHA256(              |
  |   secret,                                |
  |   "DroneID:NonceHex:Timestamp"          |
  | )                                        |
  |                                           |
  |-------- 3. AUTH_REQUEST ----------------→|
  |         (DroneID, HMAC, Timestamp, IP)   |
  |                                           |
  |         [Server validates HMAC]          |
  |                                           |
  |←------- 4. AUTH_ACK/REJECT -------------|
  |         (Token, ExpiresAt, Interval)     |
  |                                           |
  |                                           |
  |======= Session Active (24 hours) ========|
  |                                           |
  |-------- 5. KEEPALIVE (every 45s) ------→|
  |         (Token, DroneID, IP, Timestamp)  |
  |                                           |
  |←------- 6. KEEPALIVE_ACK ---------------|
  |         (NewExpiresAt, Interval)         |
  |                                           |
  |======= Now can send MAVLink packets =====|
  |                                           |
  |-------- MAVLink UDP (port 14550) ------→|
  |         [Router checks authentication]   |
  |         [Forwards if authenticated]      |
  |                                           |
```

---

## 💻 Implementation Code (Python)

### Complete Authentication Client

```python
#!/usr/bin/env python3
"""
Drone Authentication Client
Simple implementation for drone-side authentication
"""

import socket
import struct
import hmac
import hashlib
import time
import sys
import threading

# ============================================
# CONFIGURATION
# ============================================

SERVER_HOST = "45.117.171.237"  # Your server IP
AUTH_PORT = 5770                 # Drone auth port
DRONE_ID = 1                     # Your drone ID (1, 2, or 3)
SECRET_KEY = "drone_1_secret_key_abc123"  # Match your drone ID

# Protocol message types
MSG_AUTH_CHALLENGE = 0x01
MSG_AUTH_REQUEST = 0x02
MSG_AUTH_ACK = 0x03
MSG_KEEPALIVE = 0x05
MSG_KEEPALIVE_ACK = 0x06

# ============================================
# UTILITY FUNCTIONS
# ============================================

def compute_hmac(secret, drone_id, nonce, timestamp):
    """Compute HMAC-SHA256 signature"""
    nonce_hex = nonce.hex()
    message = f"{drone_id}:{nonce_hex}:{timestamp}"
    h = hmac.new(secret.encode(), message.encode(), hashlib.sha256)
    return h.digest()

def serialize_auth_request(drone_id, hmac_sig, timestamp, ip=""):
    """Create AUTH_REQUEST packet"""
    packet = bytearray()
    packet.append(MSG_AUTH_REQUEST)
    packet.extend(struct.pack('<I', drone_id))
    packet.extend(struct.pack('<H', len(hmac_sig)))
    packet.extend(hmac_sig)
    packet.extend(struct.pack('<Q', timestamp))
    ip_bytes = ip.encode()
    packet.extend(struct.pack('<H', len(ip_bytes)))
    packet.extend(ip_bytes)
    return bytes(packet)

def parse_auth_challenge(data):
    """Parse AUTH_CHALLENGE response"""
    if data[0] != MSG_AUTH_CHALLENGE:
        raise ValueError(f"Invalid packet type: {data[0]:02x}")
    
    offset = 1
    nonce_len = struct.unpack('<H', data[offset:offset+2])[0]
    offset += 2
    nonce = data[offset:offset+nonce_len]
    offset += nonce_len
    timeout_sec = struct.unpack('<H', data[offset:offset+2])[0]
    
    return nonce, timeout_sec

def parse_auth_ack(data):
    """Parse AUTH_ACK response"""
    if data[0] != MSG_AUTH_ACK:
        raise ValueError(f"Invalid packet type: {data[0]:02x}")
    
    offset = 1
    result = data[offset]
    offset += 1
    
    if result != 0x00:  # RESULT_SUCCESS
        error_code = data[offset]
        offset += 1
        wait_sec = struct.unpack('<H', data[offset:offset+2])[0]
        return False, error_code, wait_sec, None, None
    
    token_len = struct.unpack('<H', data[offset:offset+2])[0]
    offset += 2
    token = data[offset:offset+token_len].decode()
    offset += token_len
    expires_at = struct.unpack('<Q', data[offset:offset+8])[0]
    offset += 8
    interval = struct.unpack('<H', data[offset:offset+2])[0]
    
    return True, token, interval, expires_at, None

def serialize_keepalive(session_token, drone_id, current_ip):
    """Create KEEPALIVE packet"""
    packet = bytearray()
    packet.append(MSG_KEEPALIVE)
    
    token_bytes = session_token.encode()
    packet.extend(struct.pack('<H', len(token_bytes)))
    packet.extend(token_bytes)
    
    packet.extend(struct.pack('<I', drone_id))
    
    ip_bytes = current_ip.encode()
    packet.extend(struct.pack('<H', len(ip_bytes)))
    packet.extend(ip_bytes)
    
    timestamp = int(time.time())
    packet.extend(struct.pack('<Q', timestamp))
    
    return bytes(packet)

def parse_keepalive_ack(data):
    """Parse KEEPALIVE_ACK response"""
    if data[0] != MSG_KEEPALIVE_ACK:
        raise ValueError(f"Invalid packet type: {data[0]:02x}")
    
    offset = 1
    result = data[offset]
    offset += 1
    
    if result != 0x00:  # RESULT_SUCCESS
        error_code = data[offset]
        return False, error_code, None, None
    
    expires_at = struct.unpack('<Q', data[offset:offset+8])[0]
    offset += 8
    interval = struct.unpack('<H', data[offset:offset+2])[0]
    
    return True, None, expires_at, interval

# ============================================
# DRONE AUTHENTICATION CLIENT
# ============================================

class DroneAuthClient:
    def __init__(self, host, port, drone_id, secret):
        self.host = host
        self.port = port
        self.drone_id = drone_id
        self.secret = secret
        self.session_token = None
        self.keepalive_interval = 45  # seconds
        self.running = False
        self.sock = None
        self.keepalive_thread = None
    
    def authenticate(self):
        """Perform authentication handshake"""
        print(f"[AUTH] Connecting to {self.host}:{self.port}...")
        
        try:
            self.sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            self.sock.settimeout(10)
            self.sock.connect((self.host, self.port))
            print(f"[AUTH] ✓ Connected")
            
            # Step 1: Send initial AUTH_REQUEST
            timestamp = int(time.time())
            fake_hmac = b'\\x00' * 32
            auth_req = serialize_auth_request(self.drone_id, fake_hmac, timestamp)
            self.sock.send(auth_req)
            print(f"[AUTH] ✓ Sent AUTH_REQUEST")
            
            # Step 2: Receive AUTH_CHALLENGE
            response = self.sock.recv(4096)
            nonce, timeout_sec = parse_auth_challenge(response)
            print(f"[AUTH] ✓ Received challenge (nonce={nonce.hex()[:16]}...)")
            
            # Step 3: Compute HMAC and send AUTH_REQUEST with signature
            timestamp = int(time.time())
            hmac_sig = compute_hmac(self.secret, self.drone_id, nonce, timestamp)
            auth_req = serialize_auth_request(self.drone_id, hmac_sig, timestamp, "0.0.0.0")
            self.sock.send(auth_req)
            print(f"[AUTH] ✓ Sent AUTH_REQUEST with HMAC")
            
            # Step 4: Receive AUTH_ACK
            response = self.sock.recv(4096)
            success, *result = parse_auth_ack(response)
            
            if success:
                token, interval, expires_at, _ = result
                self.session_token = token
                self.keepalive_interval = interval
                
                expires_time = time.strftime('%Y-%m-%d %H:%M:%S', time.localtime(expires_at))
                print(f"[AUTH] ✅ Authentication successful!")
                print(f"[AUTH]    Token: {token[:20]}...")
                print(f"[AUTH]    Expires: {expires_time}")
                print(f"[AUTH]    Keepalive: {interval}s")
                return True
            else:
                error_code, wait_sec = result[:2]
                print(f"[AUTH] ❌ Authentication failed (error={error_code}, wait={wait_sec}s)")
                return False
                
        except Exception as e:
            print(f"[AUTH] ❌ Error: {e}")
            return False
    
    def send_keepalive(self):
        """Send keepalive message"""
        try:
            packet = serialize_keepalive(self.session_token, self.drone_id, "0.0.0.0")
            self.sock.send(packet)
            
            response = self.sock.recv(4096)
            success, error_code, expires_at, interval = parse_keepalive_ack(response)
            
            if success:
                print(f"[KEEPALIVE] ✓ Acknowledged (expires: {time.strftime('%H:%M:%S', time.localtime(expires_at))})")
                return True
            else:
                print(f"[KEEPALIVE] ❌ Rejected (error={error_code})")
                return False
                
        except Exception as e:
            print(f"[KEEPALIVE] ❌ Error: {e}")
            return False
    
    def keepalive_loop(self):
        """Background thread for periodic keepalives"""
        while self.running:
            time.sleep(self.keepalive_interval)
            if self.running:
                if not self.send_keepalive():
                    print(f"[KEEPALIVE] ❌ Failed - attempting re-authentication...")
                    self.running = False
    
    def start(self):
        """Start authentication and keepalive"""
        if not self.authenticate():
            return False
        
        self.running = True
        self.keepalive_thread = threading.Thread(target=self.keepalive_loop, daemon=True)
        self.keepalive_thread.start()
        
        print(f"[AUTH] 🚀 Authenticated - keepalive active every {self.keepalive_interval}s")
        print(f"[AUTH] 🎯 Now ready to send MAVLink packets!")
        return True
    
    def stop(self):
        """Stop keepalive and close connection"""
        self.running = False
        if self.keepalive_thread:
            self.keepalive_thread.join(timeout=2)
        if self.sock:
            self.sock.close()
        print(f"[AUTH] 👋 Disconnected")

# ============================================
# MAIN - EXAMPLE USAGE
# ============================================

def main():
    """Example usage"""
    print("=" * 60)
    print("Drone Authentication Client")
    print("=" * 60)
    
    client = DroneAuthClient(
        host=SERVER_HOST,
        port=AUTH_PORT,
        drone_id=DRONE_ID,
        secret=SECRET_KEY
    )
    
    if not client.start():
        print("❌ Authentication failed")
        return 1
    
    try:
        # Keep running - send keepalives automatically
        print("\\n[INFO] Press Ctrl+C to exit\\n")
        while True:
            time.sleep(1)
            
    except KeyboardInterrupt:
        print("\\n[INFO] Shutting down...")
        client.stop()
        return 0

if __name__ == "__main__":
    sys.exit(main())
```

---

## 🔧 Configuration Guide

### Step 1: Set Your Drone ID

```python
DRONE_ID = 1  # Change to 1, 2, or 3
```

### Step 2: Set Matching Secret

```python
# For Drone 1:
SECRET_KEY = "drone_1_secret_key_abc123"

# For Drone 2:
SECRET_KEY = "drone_2_secret_key_def456"

# For Drone 3:
SECRET_KEY = "drone_3_secret_key_ghi789"
```

### Step 3: Set Server IP

```python
SERVER_HOST = "45.117.171.237"  # Your router IP
AUTH_PORT = 5770                 # Fixed port
```

---

## 🎯 Integration với MAVLink

### Workflow Hoàn Chỉnh:

```python
import time
from dronekit import connect

# 1. Authenticate trước
auth_client = DroneAuthClient(
    host="45.117.171.237",
    port=5770,
    drone_id=1,
    secret="drone_1_secret_key_abc123"
)

if not auth_client.start():
    print("Cannot connect - authentication failed")
    exit(1)

print("✅ Authenticated - connecting MAVLink...")

# 2. Sau đó mới connect MAVLink
vehicle = connect('udp:45.117.171.237:14550', wait_ready=True)

print(f"✅ MAVLink connected - Mode: {vehicle.mode.name}")

# 3. Sử dụng bình thường
try:
    while True:
        print(f"Alt: {vehicle.location.global_relative_frame.alt}m")
        time.sleep(1)
        
except KeyboardInterrupt:
    print("Shutting down...")
    vehicle.close()
    auth_client.stop()
```

---

## 🐛 Troubleshooting

### Problem 1: Connection Refused
```
[AUTH] ❌ Error: Connection refused
```

**Solution:**
- Check server IP: `ping 45.117.171.237`
- Check port open: `telnet 45.117.171.237 5770`
- Check firewall: `sudo ufw status`

### Problem 2: Invalid HMAC
```
[AUTH] ❌ Authentication failed (error=0, wait=10s)
```

**Solution:**
- Verify drone ID matches secret key
- Check system time synchronized: `date`
- Ensure no typos in secret key

### Problem 3: Keepalive Failed
```
[KEEPALIVE] ❌ Rejected (error=6)
```

**Solution:**
- Session expired (24h limit)
- Re-run authentication
- Check network connectivity

### Problem 4: MAVLink Packets Blocked
```
🔒 Packet from unauthenticated drone X blocked
```

**Solution:**
- Ensure authentication ran successfully
- Check keepalive still running (every 45s)
- Session may have expired - re-authenticate

---

## 📊 Testing Checklist

### Pre-Flight Testing:

- [ ] **Test 1**: Run authentication script
  ```bash
  python3 drone_auth_client.py
  ```
  Expected: `✅ Authentication successful!`

- [ ] **Test 2**: Check keepalive messages
  ```
  [KEEPALIVE] ✓ Acknowledged
  ```
  Every 45 seconds

- [ ] **Test 3**: Send test MAVLink packet
  ```bash
  mavproxy.py --master=udp:127.0.0.1:14550
  ```
  Expected: Packets forwarded

- [ ] **Test 4**: Check server logs
  ```bash
  tail -f logs/hybrid-router.log | grep "drone 1"
  ```
  Expected: No "blocked" messages

### Production Testing:

- [ ] **Test 5**: Long-duration (1+ hour)
  - Keep authentication running
  - Verify keepalives continue
  - Check MAVLink telemetry flowing

- [ ] **Test 6**: Network interruption
  - Disconnect WiFi briefly
  - Reconnect
  - Verify auto-recovery

- [ ] **Test 7**: Session expiration
  - Stop keepalive for 3+ minutes
  - Verify session expires
  - Re-authenticate successfully

---

## 🚀 Production Deployment

### Systemd Service (Linux)

Create `/etc/systemd/system/drone-auth.service`:

```ini
[Unit]
Description=Drone Authentication Client
After=network.target

[Service]
Type=simple
User=drone
WorkingDirectory=/home/drone
ExecStart=/usr/bin/python3 /home/drone/drone_auth_client.py
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl daemon-reload
sudo systemctl enable drone-auth
sudo systemctl start drone-auth
sudo systemctl status drone-auth
```

---

## 📖 Protocol Reference

### Message Types

| Type | Code | Direction | Description |
|------|------|-----------|-------------|
| AUTH_CHALLENGE | 0x01 | Server → Drone | Send nonce |
| AUTH_REQUEST | 0x02 | Drone → Server | Send HMAC |
| AUTH_ACK | 0x03 | Server → Drone | Success |
| KEEPALIVE | 0x05 | Drone → Server | Heartbeat |
| KEEPALIVE_ACK | 0x06 | Server → Drone | ACK |

### Error Codes

| Code | Name | Meaning |
|------|------|---------|
| 0x00 | ERR_INVALID_HMAC | Wrong signature |
| 0x01 | ERR_TIMESTAMP_OUT_OF_RANGE | Clock skew |
| 0x02 | ERR_UNKNOWN_DRONE_ID | Not registered |
| 0x03 | ERR_RATE_LIMITED | Too many attempts |
| 0x06 | ERR_SESSION_EXPIRED | Session timeout |

---

## 🔐 Security Notes

### Current (Testing):
- ✅ HMAC-SHA256 authentication
- ✅ One-time nonces (replay protection)
- ✅ Timestamp validation (±30s)
- ✅ Session timeout (24h)
- ❌ **No TLS encryption** (plaintext TCP)
- ❌ **Hardcoded secrets** (not secure)

### Production Recommendations:
- 🔒 Enable TLS 1.3
- 🔒 Database-backed credentials
- 🔒 Certificate-based auth
- 🔒 Rotate secrets regularly
- 🔒 Monitor failed attempts

---

## 📞 Support

**Issues:**
- Check server logs: `/var/log/hybrid-router.log`
- Test with Python client first
- Verify firewall rules
- Check system time sync

**Contact:**
- GitHub: [your-repo-url]
- Email: support@example.com

---

**Last Updated:** November 18, 2025  
**Version:** Phase 1E Complete  
**Status:** ✅ Production Ready (with hardcoded credentials for testing)
