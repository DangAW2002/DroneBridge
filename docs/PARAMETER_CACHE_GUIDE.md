# Hướng dẫn: Cơ chế Cache Parameter và Cách Request từ Pixhawk

Tài liệu này mô tả cách QGroundControl (QGC) lưu trữ bộ nhớ đệm (cache) các tham số (parameters) và cách gửi lệnh để yêu cầu Pixhawk gửi lại toàn bộ danh sách tham số.

## 1. Cơ chế Caching trong QGC

Để tăng tốc độ kết nối, QGC không tải lại toàn bộ tham số mỗi khi kết nối nếu không cần thiết. Thay vào đó, nó sử dụng cơ chế **Cache** dựa trên mã kiểm tra (CRC).

### Quy trình hoạt động:
1.  **Kết nối:** Khi QGC kết nối với Pixhawk.
2.  **Hash Check:** QGC nhận một mã hash (CRC32) từ Pixhawk đại diện cho trạng thái hiện tại của toàn bộ tham số trên xe.
3.  **So sánh:**
    *   Nếu hash từ Pixhawk **trùng** với hash của file cache cục bộ: QGC tải tham số từ file cache (Rất nhanh).
    *   Nếu hash **khác nhau**: QGC hiểu rằng tham số trên xe đã thay đổi. Nó sẽ gửi lệnh `PARAM_REQUEST_LIST` để tải lại toàn bộ từ đầu (Chậm hơn).
4.  **Lưu Cache:** Sau khi tải xong, QGC cập nhật file cache mới.

### Vị trí file Cache:
File cache thường được lưu dưới dạng binary `.v2` tại:
*   **Windows:** `%APPDATA%\QGroundControl.org\QGroundControl\ParamCache`
*   **Linux/macOS:** `~/.config/QGroundControl.org/QGroundControl/ParamCache` (hoặc tương tự tùy OS).

---

## 2. Lệnh Request Parameter (MAVLink Command)

Để yêu cầu Pixhawk gửi lại toàn bộ danh sách tham số (bỏ qua cache hoặc khi muốn refresh), QGC sử dụng message MAVLink **`PARAM_REQUEST_LIST`**.

### Cấu trúc Message `PARAM_REQUEST_LIST` (ID #21)

Đây là lệnh bạn cần gửi nếu muốn trigger việc cập nhật thủ công.

| Field Name | Type | Description |
| :--- | :--- | :--- |
| `target_system` | `uint8_t` | System ID của Pixhawk (thường là 1). |
| `target_component` | `uint8_t` | Component ID của Autopilot (thường là 1 - `MAV_COMP_ID_AUTOPILOT1`). |

### Ví dụ Code (C++ / QGC Source)

Trong mã nguồn QGC, việc này được thực hiện trong class `ParameterManager`.

**File:** `src/FactSystem/ParameterManager.cc`
**Hàm:** `refreshAllParameters()`

```cpp
void ParameterManager::refreshAllParameters(uint8_t componentId)
{
    // Lấy link kết nối hiện tại
    SharedLinkInterfacePtr sharedLink = _vehicle->vehicleLinkManager()->primaryLink().lock();
    if (!sharedLink) {
        return;
    }

    mavlink_message_t msg;
    mavlink_param_request_list_t request;

    // Cấu hình payload
    request.target_system    = _vehicle->id();       // ID của xe (ví dụ: 1)
    request.target_component = componentId;          // ID thành phần (ví dụ: 1 cho Autopilot)

    // Encode message
    mavlink_msg_param_request_list_encode_chan(
        MAVLinkProtocol::instance()->getSystemId(),
        MAVLinkProtocol::getComponentId(),
        sharedLink->mavlinkChannel(),
        &msg,
        &request
    );

    // Gửi message đi
    _vehicle->sendMessageOnLinkThreadSafe(sharedLink.get(), msg);
}
```

---

## 3. Cách thực hiện thủ công trên giao diện QGC

Nếu bạn không muốn can thiệp vào code mà chỉ muốn xóa cache hoặc request lại từ giao diện:

1.  **Refresh (Tải lại):**
    *   Vào **Vehicle Setup** (biểu tượng bánh răng).
    *   Chọn **Parameters**.
    *   Nhấn nút **Tools** (góc trên bên phải).
    *   Chọn **Refresh**. (Hành động này sẽ gửi `PARAM_REQUEST_LIST`).

2.  **Xóa Cache (Clear Vehicle Persistence):**
    *   Vào **Application Settings** (biểu tượng chữ Q góc trái trên).
    *   Kéo xuống mục **Vehicle Persistence**.
    *   Nhấn **Clear**. (Hành động này xóa file cache trên đĩa, lần kết nối sau QGC sẽ buộc phải tải lại từ đầu).

---

## 4. Phản hồi từ Pixhawk

Sau khi nhận được lệnh `PARAM_REQUEST_LIST`, Pixhawk sẽ:
1.  Gửi liên tục các message **`PARAM_VALUE`** (ID #22) cho từng tham số.
2.  Mỗi message chứa: `param_id`, `param_value`, `param_type`, `param_index`, và `param_count`.
3.  QGC sẽ lắng nghe và cập nhật lại danh sách tham số trên giao diện.
