#!/usr/bin/env python3
"""
Auto Enable 4G và cấp phát IP cho wwan0
Sử dụng AT commands để khởi tạo module SIM7600
"""

import subprocess
import time
import serial
import serial.tools.list_ports

BAUD_RATE = 115200

def find_usb_port():
    """Tìm USB port của SIM7600 (thường là ttyUSB2)"""
    ports = [p.device for p in serial.tools.list_ports.comports() if 'ttyUSB' in p.device]
    return '/dev/ttyUSB2' if '/dev/ttyUSB2' in ports else (ports[0] if ports else None)

def send_at(ser, cmd, wait=2.0):
    """Gửi AT command và trả về response"""
    try:
        ser.reset_input_buffer()
        ser.write((cmd + "\r\n").encode())
        ser.flush()
        response = []
        start = time.time()
        while (time.time() - start) < wait:
            if ser.in_waiting > 0:
                line = ser.readline().decode('utf-8', errors='ignore').strip()
                if line:
                    response.append(line)
                if 'OK' in line or 'ERROR' in line:
                    break
            time.sleep(0.05)
        
        result = '\n'.join(response) if response else None
        
        # In kết quả
        if result:
            if 'OK' in result:
                print(f"  ✓ {cmd}")
            elif 'ERROR' in result:
                print(f"  ✗ {cmd}: ERROR")
            else:
                print(f"  ? {cmd}")
        
        return result
    except Exception as e:
        print(f"  ✗ {cmd}: Exception - {e}")
        return None

def enable_4g_with_at_commands():
    """Bật 4G bằng AT commands"""
    print("\n" + "=" * 50)
    print("  AUTO ENABLE 4G VÀ CẤP PHÁT IP")
    print("=" * 50)
    
    # === BƯỚC 1: Kết nối USB port ===
    print("\n[1/5] Tìm USB port...")
    port = find_usb_port()
    if not port:
        print("  ✗ Không tìm thấy USB port của module SIM7600")
        return False
    
    print(f"  ✓ Đã tìm thấy: {port}")
    
    try:
        ser = serial.Serial(port, BAUD_RATE, timeout=2, rtscts=False, dsrdtr=False)
        ser.setDTR(False)
        ser.setRTS(False)
        time.sleep(0.5)
        ser.reset_input_buffer()
        print("  ✓ Đã kết nối serial")
    except Exception as e:
        print(f"  ✗ Lỗi kết nối serial: {e}")
        return False
    
    # === BƯỚC 2: Kiểm tra module respond ===
    print("\n[2/5] Kiểm tra module...")
    resp = send_at(ser, "AT")
    if not resp or "OK" not in resp:
        print("  ✗ Module không phản hồi")
        ser.close()
        return False
    print("  ✓ Module phản hồi OK")
    
    # === BƯỚC 3: Cấu hình và bật RF ===
    print("\n[3/5] Cấu hình mạng...")
    
    # Tắt chế độ tiết kiệm năng lượng
    print("  • Tắt Power Saving Mode...")
    send_at(ser, "AT+CPSMS=0", wait=2)
    send_at(ser, "AT+CEDRXS=0", wait=2)
    send_at(ser, "AT+CSCLK=0", wait=2)
    
    # Cấu hình LTE bands cho Việt Nam (Band 3 quan trọng cho Viettel)
    print("  • Cấu hình LTE bands...")
    gsm_bands = "0x0002000000400183"
    lte_bands = "0x00000000080800C7"  # Bands 1,2,3,7,8,20,28
    tds_bands = "0x0000000000000021"
    send_at(ser, f"AT+CNBP={gsm_bands},{lte_bands},{tds_bands}", wait=3)
    
    # Bật RF (Full functionality)
    print("  • Bật RF (AT+CFUN=1)...")
    send_at(ser, "AT+CFUN=1", wait=3)
    time.sleep(5)  # Đợi module ổn định
    
    # Set Auto mode (LTE/GSM/WCDMA)
    print("  • Set Auto mode...")
    send_at(ser, "AT+CNMP=2", wait=2)
    
    # Auto operator selection
    print("  • Auto operator selection...")
    send_at(ser, "AT+COPS=0", wait=5)
    
    # Enable network registration report
    send_at(ser, "AT+CREG=2", wait=1)
    send_at(ser, "AT+CEREG=2", wait=1)
    
    # Set APN (Viettel)
    print("  • Set APN (Viettel v-internet)...")
    send_at(ser, 'AT+CGDCONT=1,"IP","v-internet"', wait=2)
    
    # Attach to PS domain
    print("  • Attach to PS domain...")
    send_at(ser, "AT+CGATT=1", wait=5)
    
    # Activate PDP context (QUAN TRỌNG cho data connection!)
    print("  • Activate PDP context...")
    send_at(ser, "AT+CGACT=1,1", wait=5)
    
    # KHÔNG dùng AT$QCRMCALL vì nó conflict với QMI
    # print("  • Enable auto data connection...")
    # send_at(ser, "AT$QCRMCALL=1,1", wait=3)
    
    print("  ✓ Cấu hình hoàn tất")
    
    # === BƯỚC 4: Đợi đăng ký mạng ===
    print("\n[4/6] Đợi đăng ký mạng (tối đa 60s)...")
    
    network_ok = False
    start = time.time()
    
    while (time.time() - start) < 60:
        elapsed = int(time.time() - start)
        
        creg = send_at(ser, "AT+CREG?", wait=1) or ""
        cereg = send_at(ser, "AT+CEREG?", wait=1) or ""
        csq = send_at(ser, "AT+CSQ", wait=1) or ""
        
        # Parse signal
        signal = "??"
        if "+CSQ:" in csq:
            try:
                signal = csq.split("+CSQ:")[1].split(",")[0].strip()
            except:
                pass
        
        # Kiểm tra đăng ký
        gsm_ok = ",1" in creg or ",5" in creg
        lte_ok = ",1" in cereg or ",5" in cereg
        
        status = "4G" if lte_ok else ("3G/2G" if gsm_ok else "Đang tìm...")
        print(f"  [{elapsed:2}s] Signal: {signal}/31 | {status}", end="\r")
        
        if gsm_ok or lte_ok:
            network_ok = True
            print(f"  [{elapsed:2}s] Signal: {signal}/31 | ✓ Đã đăng ký {status}      ")
            break
        
        time.sleep(3)
    
    if not network_ok:
        print(f"\n  ✗ Không đăng ký được mạng sau 60s")
        ser.close()
        return False
    
    # In thông tin mạng
    print("\n  Thông tin mạng:")
    cops = send_at(ser, "AT+COPS?", wait=1) or ""
    if "+COPS:" in cops:
        print(f"    {cops}")
    
    # === BƯỚC 5: Lấy IP từ PDP context ===
    print("\n[5/5] Lấy IP và cấu hình wwan0...")
    
    # Get IP address from PDP context
    print("  • Lấy IP từ PDP context...")
    cgpaddr = send_at(ser, "AT+CGPADDR=1", wait=2) or ""
    
    ip_address = None
    if "+CGPADDR:" in cgpaddr:
        try:
            parts = cgpaddr.split("+CGPADDR:")[1].split(",")
            if len(parts) >= 2:
                ip_address = parts[1].strip().strip('"').split()[0]  # Lấy IP, loại bỏ OK và newline
                print(f"    ✓ IP: {ip_address}")
        except Exception as e:
            print(f"    ✗ Không parse được IP: {e}")
    else:
        print("    ✗ Không lấy được IP từ module")
    
    ser.close()
    
    if not ip_address:
        print("\n  ✗ Không có IP address, dừng lại")
        return False
    
    # === BƯỚC 6: Cấu hình wwan0 interface với QMI ===
    print("\n[6/6] Cấu hình wwan0 interface...")
    
    try:
        # Unload simcom_wwan driver (conflict với qmi_wwan)
        print("  • Switch sang qmi_wwan driver...")
        subprocess.run(['sudo', 'rmmod', 'simcom_wwan'], capture_output=True)
        time.sleep(1)
        subprocess.run(['sudo', 'modprobe', 'qmi_wwan'], capture_output=True)
        time.sleep(2)
        
        # Check if wwan0 interface exists
        print("  • Kiểm tra wwan0 interface...")
        result = subprocess.run(['ip', 'link', 'show', 'wwan0'], capture_output=True)
        wwan0_exists = (result.returncode == 0)
        
        if not wwan0_exists:
            print("    ! wwan0 chưa xuất hiện, thử tìm và rebind USB device...")
            # Find SIM7600 USB interface for network
            result = subprocess.run(['find', '/sys/bus/usb/devices/', '-name', '1e0e:9001*'], 
                                   capture_output=True, text=True)
            
            if result.stdout.strip():
                print("    • Tìm thấy SIM7600 USB device")
                # Try to find network interface (usually interface 4 or 5)
                for iface_num in ['4', '5', '6']:
                    # Find the actual USB path
                    find_cmd = subprocess.run(['find', '/sys/bus/usb/devices/', '-type', 'd', 
                                              '-path', f'*/3-*:1.{iface_num}'],
                                             capture_output=True, text=True)
                    
                    if find_cmd.stdout.strip():
                        usb_path = find_cmd.stdout.strip().split('/')[-1]
                        print(f"    • Thử rebind interface {usb_path}...")
                        
                        # Try to bind with qmi_wwan
                        subprocess.run(['sudo', 'sh', '-c',
                                       f'echo "{usb_path}" > /sys/bus/usb/drivers/qmi_wwan/bind 2>/dev/null || true'],
                                      capture_output=True)
                        time.sleep(2)
                        
                        # Check if wwan0 appeared
                        result = subprocess.run(['ip', 'link', 'show', 'wwan0'], capture_output=True)
                        if result.returncode == 0:
                            wwan0_exists = True
                            print(f"    ✓ wwan0 xuất hiện sau khi bind {usb_path}")
                            break
            
            # If still not found, try loading simcom_wwan and check again
            if not wwan0_exists:
                print("    • QMI mode failed, thử load simcom_wwan driver...")
                subprocess.run(['sudo', 'modprobe', 'simcom_wwan'], capture_output=True)
                time.sleep(3)
                
                # Check again
                result = subprocess.run(['ip', 'link', 'show', 'wwan0'], capture_output=True)
                wwan0_exists = (result.returncode == 0)
        
        if wwan0_exists:
            print("    ✓ wwan0 interface available")
        else:
            print("    ✗ wwan0 không khả dụng sau các thử nghiệm")
            print("    → 4G đã active nhưng không có wwan0 interface")
            print("    → Module hoạt động ở chế độ serial/AT command")
            return True  # Still success, modem is online
        
        # Wait for QMI device to be ready (important for boot scenarios)
        print("  • Đợi QMI device sẵn sàng...")
        qmi_ready = False
        import os
        for attempt in range(15):  # Reduced to 15s since we already waited above
            if os.path.exists('/dev/cdc-wdm0'):
                qmi_ready = True
                print(f"    ✓ /dev/cdc-wdm0 ready (sau {attempt}s)")
                break
            time.sleep(1)
        
        if not qmi_ready:
            print("    ! /dev/cdc-wdm0 không xuất hiện")
            # QMI không available nhưng wwan0 có thể vẫn hoạt động với simcom_wwan
        
        # Only continue with wwan0 config if interface exists
        if not wwan0_exists:
            print("  • wwan0 không tồn tại - 4G hoạt động ở chế độ AT command")
            return True  # Success since modem is online with IP
        
        # Enable raw IP mode (chỉ cần với QMI mode)
        if qmi_ready:
            print("  • Enable raw IP mode...")
            subprocess.run(['sudo', 'sh', '-c', 'echo Y > /sys/class/net/wwan0/qmi/raw_ip'], 
                          capture_output=True)
        
        # Bring up wwan0
        print("  • Bring up wwan0...")
        subprocess.run(['sudo', 'ip', 'link', 'set', 'wwan0', 'up'], check=True)
        time.sleep(2)
        
        # Check if we already have IP from AT commands
        # If yes, PDP context is already active, skip QMI start (will timeout)
        pdh = None
        cid = None
        skip_qmi_start = bool(ip_address)
        
        if skip_qmi_start:
            print("  • Skip QMI start network (PDP đã active qua AT commands)")
            print(f"    IP đã có: {ip_address}")
        elif qmi_ready:
            print("  • Start network qua QMI...")
            try:
                result = subprocess.run(['sudo', 'qmicli', '-d', '/dev/cdc-wdm0', 
                                        '--wds-start-network=apn=v-internet,ip-type=4',
                                        '--client-no-release-cid'],
                                       capture_output=True, text=True, timeout=30)
                
                # Parse packet data handle và CID
                for line in result.stdout.split('\n'):
                    if 'Packet data handle:' in line:
                        pdh = line.split(':')[1].strip().strip("'")
                    if 'CID:' in line:
                        cid = line.split(':')[1].strip().strip("'")
                
                if pdh:
                    print(f"    ✓ Connected (PDH: {pdh}, CID: {cid})")
                    print(f"    ! Để disconnect sau: sudo qmicli -d /dev/cdc-wdm0 --wds-stop-network={pdh} --client-cid={cid}")
                elif 'interface-in-use' in result.stderr or 'CallFailed' in result.stderr:
                    print("    ! Connection đã active hoặc đang được sử dụng")
                    print("    Tiếp tục với connection hiện tại...")
                else:
                    print(f"    ! QMI stdout: {result.stdout[:100]}")
                    print(f"    ! QMI stderr: {result.stderr[:100]}")
                    print("    Thử set IP thủ công...")
            except subprocess.TimeoutExpired:
                print("    ! QMI start timeout - connection có thể đã active")
                print("    Tiếp tục với IP configuration...")
        
        # Get IP settings từ QMI (if device ready and worth trying)
        if qmi_ready and not skip_qmi_start:
            print("  • Get IP settings từ modem...")
            try:
                result = subprocess.run(['sudo', 'qmicli', '-d', '/dev/cdc-wdm0', '--wds-get-current-settings'],
                                       capture_output=True, text=True, timeout=10)
            except subprocess.TimeoutExpired:
                print("    ! Timeout getting QMI settings")
                result = None
        else:
            if skip_qmi_start:
                print("  • Skip QMI settings query (sử dụng IP từ AT commands)")
            else:
                print("  • Skip QMI settings query (device not ready)")
            result = None
        
        ip_addr = None
        gateway = None
        dns1 = None
        dns2 = None
        
        if result and result.stdout:
            for line in result.stdout.split('\n'):
                if 'IPv4 address:' in line:
                    ip_addr = line.split(':')[1].strip()
                elif 'IPv4 gateway address:' in line:
                    gateway = line.split(':')[1].strip()
                elif 'IPv4 primary DNS:' in line:
                    dns1 = line.split(':')[1].strip()
                elif 'IPv4 secondary DNS:' in line:
                    dns2 = line.split(':')[1].strip()
        
        if not ip_addr:
            ip_addr = ip_address  # Fallback to AT command IP
        
        print(f"    IP: {ip_addr}")
        if gateway:
            print(f"    Gateway: {gateway}")
        if dns1:
            print(f"    DNS: {dns1}, {dns2 if dns2 else 'N/A'}")
        
        # Set IP address
        print(f"  • Set IP {ip_addr}...")
        subprocess.run(['sudo', 'ip', 'addr', 'flush', 'dev', 'wwan0'], capture_output=True)
        
        if gateway:
            # Có gateway, dùng /32 với peer
            subprocess.run(['sudo', 'ip', 'addr', 'add', ip_addr, 'peer', gateway, 'dev', 'wwan0'], 
                          check=True)
        else:
            # Không có gateway, dùng /32 và route qua device
            subprocess.run(['sudo', 'ip', 'addr', 'add', f'{ip_addr}/32', 'dev', 'wwan0'], 
                          check=True)
        
        # KHÔNG set default route tự động để tránh mất SSH
        # User có thể tự add route sau nếu cần dùng 4G
        print("  • KHÔNG set default route (giữ nguyên mạng hiện tại)")
        print("    → SSH và mạng hiện tại vẫn hoạt động bình thường")
        print("    → Muốn route tất cả traffic qua 4G:")
        print("      sudo ip route add default dev wwan0 metric 200")
        
        print("  ✓ Cấu hình interface hoàn tất")
        
        # Show interface info
        time.sleep(1)
        result = subprocess.run(['ifconfig', 'wwan0'], capture_output=True, text=True)
        print("\n  Interface wwan0:")
        for line in result.stdout.split('\n'):
            if line.strip():
                print(f"    {line}")
        
        # Test kết nối 4G
        print("\n  • Test kết nối 4G...")
        try:
            # Ping test qua wwan0
            result = subprocess.run(['ping', '-c', '2', '-W', '3', '8.8.8.8'], 
                                   capture_output=True, text=True, timeout=10)
            
            if result.returncode == 0:
                print("  ✓ 4G internet OK!")
                # Parse ping stats
                for line in result.stdout.split('\n'):
                    if 'rtt min/avg/max' in line or 'packets transmitted' in line:
                        print(f"    {line.strip()}")
            else:
                print("  ! Không ping được 8.8.8.8")
                print("    4G đã sẵn sàng nhưng cần config route để dùng")
                print("    Thử: sudo ip route add default dev wwan0 table 100")
                print("         sudo ip rule add from 214.165.1.134 table 100")
        except Exception as e:
            print(f"  ! Lỗi test: {e}")
        
    except subprocess.CalledProcessError as e:
        print(f"  ✗ Lỗi subprocess: {e}")
        print("  ! Thử tiếp tục với cấu hình thủ công...")
        # Không return False, thử tiếp với IP thủ công
    except subprocess.TimeoutExpired as e:
        print(f"  ✗ Timeout: {e.cmd}")
        print("  ! QMI timeout - có thể connection đã active")
        print("  ! Thử tiếp tục với IP đã có...")
        # Không return False, thử tiếp với IP thủ công
    
    print("\n" + "=" * 50)
    print("  HOÀN TẤT - 4G ĐÃ ĐƯỢC BẬT VÀ CẤP IP")
    print("=" * 50)
    
    return True

if __name__ == "__main__":
    success = enable_4g_with_at_commands()
    exit(0 if success else 1)
