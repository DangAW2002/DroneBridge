# Parameter Editor Guide

## Overview

Parameter Editor là giao diện web cho phép bạn xem, tìm kiếm, và chỉnh sửa các tham số PX4 trực tiếp từ Pixhawk.

## Truy Cập

1. Mở trình duyệt và truy cập: `http://[device-ip]:8080`
2. Từ Dashboard, nhấn **Parameter Editor** hoặc truy cập trực tiếp `/params.html`

## Giao Diện

### Thanh Công Cụ (Toolbar)
- **Search Box**: Tìm kiếm parameters theo tên hoặc mô tả
- **Clear**: Xóa từ khóa tìm kiếm
- **Show modified only**: Chỉ hiển thị parameters đã được thay đổi so với default
- **Refresh**: Yêu cầu load lại parameters từ Pixhawk (PARAM_REQUEST_LIST)
- **Connection Status**: Hiển thị trạng thái kết nối tới Pixhawk

### Thanh Bên (Sidebar)
Danh sách các nhóm parameters được phân theo 3 danh mục:
- **Standard**: Các tham số tiêu chuẩn
- **System**: Các tham số hệ thống
- **Developer**: Các tham số dành cho nhà phát triển

Nhấn vào category để mở rộng/thu gọn. Nhấn vào group để hiển thị parameters.

### Bảng Parameters
- **Name**: Tên của parameter
- **Value**: Giá trị hiện tại
- **Description**: Mô tả ngắn gọn

Nhấn vào bất kỳ hàng nào để mở modal chỉnh sửa.

## Chỉnh Sửa Parameters

### Mở Modal
Nhấn vào bất kỳ hàng parameter nào trong bảng.

### Loại Input

**Boolean Parameters** (MAV_0_RADIO_CTL, etc.)
- Dropdown với 2 lựa chọn: `Disabled` / `Enabled`
- Hiển thị giá trị numeric (0/1) đằng sau tên parameter

**Enum Parameters** (SCH16T_ACC_FILT, etc.)
- Dropdown với danh sách các lựa chọn có nhãn
- Ví dụ: "6: No filter", "0: 13 Hz", "1: 30 Hz", v.v.
- Giá trị hiển thị sẽ là nhãn text thay vì số

**Bitmask Parameters**
- Danh sách checkbox cho từng bit
- Hiển thị giá trị hex và binary
- Có thể chọn nhiều bits cùng lúc

**Numeric Parameters** (INT/FLOAT)
- Input field với min/max validation
- Hiển thị unit nếu có (m/s, degrees, v.v.)

### Nút Hành Động

- **Reset to Default**: Đặt lại giá trị về default từ XML metadata
- **Send to Vehicle**: Gửi giá trị mới tới Pixhawk qua MAVLink PARAM_SET

### Feedback

Sau khi nhấn "Send to Vehicle":
1. Button chuyển sang trạng thái loading ("Sending...")
2. Nếu thành công:
   - Button hiển thị tick (✓ Success)
   - Modal tự động đóng sau 2 giây
   - Giá trị trong bảng cập nhật ngay lập tức
3. Nếu thất bại:
   - Button hiển thị X (✗ Failed)
   - Thông báo lỗi được hiển thị
   - Modal vẫn mở để bạn thử lại

## Loading Parameters

### Tự Động
Khi mở trang Parameter Editor lần đầu:
- Nếu có cached parameters từ lần truy cập trước → Sử dụng luôn
- Nếu không có cache → Gửi PARAM_REQUEST_LIST tới Pixhawk

### Thủ Công
Nhấn nút **Refresh** để gửi PARAM_REQUEST_LIST:
- Xóa cache hiện tại
- Yêu cầu Pixhawk gửi lại toàn bộ ~1137 parameters
- Hiển thị progress bar: "Loading parameters... 500/1137 (44%)"

## Real-Time Updates

Frontend tự động polling để cập nhật values từ Pixhawk cache:
- Mỗi 1 giây, check xem có parameters nào thay đổi không
- Nếu có thay đổi, bảng sẽ tự động re-render
- Không cần reload trang

## Tips

1. **Tìm kiếm nhanh**: Dùng Search Box để tìm parameter theo tên
   - Ví dụ: "RADIO", "BATT", "COMPASS"

2. **Xem chỉ các thay đổi**: Check "Show modified only" để chỉ xem parameters đã sửa

3. **Reset toàn bộ**: Không có tính năng reset toàn bộ, phải reset từng parameter một

4. **Lưu ý khi chỉnh sửa**:
   - Không nên sửa khi vehicle đang bay
   - Một số parameters yêu cầu reboot vehicle (được báo bằng icon ⚠️)

5. **Enum vs Numeric**:
   - Để ý loại input field
   - Enum parameters có dropdown, không phải input text
   - Lựa chọn từ dropdown, không tự type

## Các API Endpoints

Nếu bạn muốn integrate hoặc debug:

- `GET /api/param/status` - Trạng thái loading parameters
- `GET /api/param/list` - Danh sách toàn bộ cached parameters
- `GET /api/param/get?name=MAV_SYS_ID` - Lấy 1 parameter cụ thể
- `POST /api/param/request-list` - Gửi PARAM_REQUEST_LIST
- `POST /api/param/set` - Gửi PARAM_SET

Body cho `/api/param/set`:
```json
{
  "paramName": "MAV_RADIO_TOUT",
  "paramValue": 20,
  "paramType": "INT32"
}
```

## Troubleshooting

**"Disconnected" status**
- Kiểm tra Pixhawk có kết nối tới MAVLink listener port (14550)?
- Kiểm tra Pixhawk có gửi heartbeat?

**Parameters không load**
- Kiểm tra web console (F12) có lỗi nào không?
- Thử click "Refresh" lại
- Kiểm tra network tab xem API endpoints có respond không?

**Parameter không update trên vehicle**
- Kiểm tra lại giá trị min/max
- Kiểm tra vehicle log xem có lỗi gì?
- Thử restart Pixhawk

**UI hiển thị số thay vì label enum**
- Có thể XML metadata không đầy đủ
- Refresh lại trang
