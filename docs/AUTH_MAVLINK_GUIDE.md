# Hướng dẫn Build, Run và Cấu Hình DroneBridge

Tài liệu này hướng dẫn chi tiết cách build, chạy và cấu hình ứng dụng HBQConnect DroneBridge.

## 1. Yêu cầu hệ thống (Prerequisites)

- **Go**: Phiên bản 1.20 trở lên.
- **Make**: (Tùy chọn) Để sử dụng các lệnh shortcut trong Makefile.

## 2. Hướng dẫn Build và Run

Dự án đi kèm với `Makefile` để đơn giản hóa quá trình phát triển và triển khai.

### Cài đặt thư viện (Dependencies)
Trước khi build, hãy tải các thư viện cần thiết:
```bash
make install
# Hoặc chạy thủ công:
go mod download && go mod tidy
```

### Build ứng dụng
Để build ra file thực thi (binary):
```bash
make build
```
Sau khi build thành công, file `dronebridge` (hoặc `dronebridge.exe` trên Windows) sẽ được tạo ra tại thư mục gốc.

### Chạy ứng dụng (Run)
Có hai cách để chạy ứng dụng:

### Các chế độ chạy (Run Modes)

Chương trình hỗ trợ 2 chế độ chạy chính thông qua flag:

#### 1. Chế độ chạy thường (Normal Mode)
Mặc định (không có cờ `--register`), chương trình sẽ chạy ở chế độ xác thực thông thường:
- Đọc `uuid` và `secret` từ file cấu hình hoặc file `.drone_secret`.
- Thực hiện xác thực (Authentication) với Server.
- Nếu xác thực thành công, bắt đầu forward MAVLink.
```bash
./dronebridge
```

#### 2. Chế độ đăng ký (Registration Mode)
Sử dụng cờ `--register` để đăng ký drone mới với Fleet Server:
- **Yêu cầu**: Phải có `shared_secret` trong file cấu hình.
- Quy trình:
    1. Kết nối tới Server.
    2. Gửi yêu cầu đăng ký kèm UUID và Shared Secret.
    3. Nhận về **Secret Key** riêng cho drone này.
    4. Lưu Secret Key vào file `.drone_secret`.
    5. **Tự động chuyển sang chế độ chạy thường** ngay sau khi đăng ký thành công.
```bash
make run-register
# Hoặc:
./dronebridge --register
```

### Chạy với file cấu hình tùy chỉnh
```bash
make run-custom CONFIG=path/to/your_config.yaml
# Hoặc:
./dronebridge -config path/to/your_config.yaml
```

### Dọn dẹp (Clean)
Xóa các file build cũ:
```bash
make clean
```

## 3. Hướng dẫn Cấu hình (Configuration)

File cấu hình chính là `config.yaml`. Dưới đây là giải thích chi tiết các tham số cấu hình.

### 3.1 Cấu trúc file `config.yaml`

```yaml
# Cấu hình Logging
log:
  level: "info"                          # Mức độ log: debug, info, warn, error

# Cấu hình Xác thực (Authentication)
auth:
  enabled: true                          # Bật/Tắt tính năng xác thực với Server
  host: "45.117.171.237"                 # Địa chỉ IP của Server Xác thực (Router)
  port: 5770                             # Port xác thực (Mặc định: 5770)
  
  # --- Cấu hình Định danh & Auto-Register ---
  # UUID: Định danh duy nhất của Drone.
  # - Nếu để trống (""): Hệ thống tự động lấy Hardware ID (MAC Address) làm UUID.
  # - Nếu có giá trị: Sử dụng giá trị đó làm UUID cố định.
  uuid: ""  
  
  # Secret: Khóa bí mật dùng cho xác thực.
  # Đối với Auto-Registration, đây là Shared Secret của cả đội bay (Fleet Key).
  # ⚠️ QUAN TRỌNG: Bảo mật key này, không chia sẻ công khai.
  secret: "YOUR_SHARED_SECRET_KEY_HERE"
  
  # Cấu hình Keepalive/Heartbeat
  keepalive_interval: 30                 # Chu kỳ gửi gói tin giữ kết nối TCP (giây)
  udp_heartbeat_frequency: 2             # Tần suất gửi UDP Heartbeat (Hz - lần/giây)

# Cấu hình Mạng (Kết nối Server)
network:
  local_listen_port: 14550               # Port lắng nghe MAVLink từ Flight Controller (Pixhawk)
  target_host: "45.117.171.237"          # Địa chỉ IP Server đích để forward MAVLink tới
  target_port: 14550                     # Port UDP của Server đích
  protocol: "udp"                        # Giao thức (hiện tại hỗ trợ udp)

# Cấu hình Ethernet (Kết nối Pixhawk)
# Để trống các trường để hệ thống tự động phát hiện (Auto-detect)
ethernet:
  interface: ""                          # Tên card mạng (vd: eth0, wlan0). Để trống = tự chọn.
  local_ip: "10.41.10.1"                 # IP tĩnh muốn gán cho DroneBridge.
  broadcast_ip: ""                       # IP Broadcast (Để trống = tự tính toán)
  pixhawk_ip: "10.41.10.2"               # IP của Pixhawk (để lọc gói tin loopback)
  auto_setup: true                       # Tự động cấu hình IP cho interface khi khởi động
  subnet: "24"                           # Subnet mask (24 tương đương /24 hay 255.255.255.0)

# Cấu hình Web Server (Status Page)
web:
  port: 8080                             # Port cho trang web trạng thái
```

### 3.2 Giải thích chi tiết các mục quan trọng

#### Authentication (auth)
- **`uuid`**: Trong môi trường production hoặc khi deploy số lượng lớn, nên để trống để tận dụng tính năng Auto-Registration. Drone sẽ tự sinh UUID dựa trên phần cứng, giúp không bị trùng lặp.
- **`secret`**: Đây là khóa dùng để xác thực ban đầu. Router sẽ kiểm tra cặp `uuid` và `secret`. Nếu UUID chưa tồn tại và `secret` khớp với cấu hình server, drone sẽ được đăng ký mới.

#### Networking (network & ethernet)
- **`network`**: Định nghĩa nơi dữ liệu MAVLink sẽ được gửi đến (thường là Cloud Server).
- **`ethernet`**: Cấu hình kết nối vật lý giữa máy tính nhúng (RPi/Jetson) và Flight Controller (Pixhawk). Chế độ `auto_setup: true` rất hữu ích để tự động gán IP `10.41.10.1` giúp giao tiếp với Pixhawk (thường mặc định là `10.41.10.2` qua Ethernet).

## 4. Các lệnh Make hỗ trợ khác

- `make help`: Hiển thị danh sách các lệnh hỗ trợ.

---
**Lưu ý:** File `config.yaml` chứa thông tin nhạy cảm (secret key). Không commit file config chứa key thật lên source control công khai.
