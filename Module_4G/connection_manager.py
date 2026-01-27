#!/usr/bin/env python3
"""
Connection Manager - Auto switch between 4G and WiFi  
Priority-based routing with smart failover
Uses metric to control priority without deleting other routes
"""

import subprocess
import time
import json
import re
import os
import sys
from datetime import datetime

class ConnectionManager:
    def __init__(self):
        self.status_file = "/home/pi/HBQ_server_drone/data/connection_status.json"
        self.config_file = "/home/pi/HBQ_server_drone/data/connection_config.json"
        self.current_connection = None
        self.wifi_ssid = os.getenv('WIFI_SSID', 'HoangBachQuan')
        self.wifi_pass = os.getenv('WIFI_PASS', '')
        self.load_config()
    
    def load_config(self):
        """Load connection priority config"""
        try:
            if os.path.exists(self.config_file):
                with open(self.config_file, 'r') as f:
                    config = json.load(f)
                    self.priority = config.get('priority', '4g')
            else:
                self.priority = '4g'
        except:
            self.priority = '4g'
    
    def save_config(self, priority):
        """Save connection priority config"""
        try:
            config = {'priority': priority}
            with open(self.config_file, 'w') as f:
                json.dump(config, f, indent=2)
            self.priority = priority
            return True
        except Exception as e:
            print(f"Error saving config: {e}")
            return False
        
    def check_interface(self, iface):
        """Check if interface has IP and is UP"""
        try:
            result = subprocess.run(['ip', 'addr', 'show', iface], 
                                  capture_output=True, text=True, timeout=5)
            
            if result.returncode != 0:
                return None
            
            output = result.stdout
            
            # Check if interface is UP
            if 'state UP' not in output and 'state UNKNOWN' not in output:
                return None
            
            # Get IP address
            match = re.search(r'inet (\d+\.\d+\.\d+\.\d+)/(\d+)', output)
            if match:
                return {
                    'interface': iface,
                    'ip': match.group(1),
                    'netmask': match.group(2),
                    'state': 'UP'
                }
            
            return None
        except Exception as e:
            return None
    
    def ping_test(self, host='8.8.8.8', count=2):
        """Test internet connectivity"""
        try:
            result = subprocess.run(['ping', '-c', str(count), '-W', '3', host],
                                  capture_output=True, timeout=10)
            return result.returncode == 0
        except:
            return False
    
    def setup_4g(self):
        """Setup 4G connection using QMI - DOES NOT DELETE OTHER ROUTES"""
        print("Setting up 4G connection...")
        
        try:
            # Start network with qmicli
            result = subprocess.run([
                'sudo', 'qmicli', '-d', '/dev/cdc-wdm0',
                '--wds-start-network=apn=\'e-connect\',ip-type=4',
                '--client-no-release-cid'
            ], capture_output=True, text=True, timeout=30)
            
            if 'Network started' not in result.stdout:
                print("✗ Failed to start 4G network")
                return False
            
            print("✓ 4G network started")
            time.sleep(2)
            
            # Get IP settings
            result = subprocess.run([
                'sudo', 'qmicli', '-d', '/dev/cdc-wdm0',
                '--wds-get-current-settings'
            ], capture_output=True, text=True, timeout=10)
            
            output = result.stdout
            
            # Parse IP settings
            ip_match = re.search(r'IPv4 address: (\d+\.\d+\.\d+\.\d+)', output)
            mask_match = re.search(r'IPv4 subnet mask: (\d+\.\d+\.\d+\.\d+)', output)
            gateway_match = re.search(r'IPv4 gateway address: (\d+\.\d+\.\d+\.\d+)', output)
            dns1_match = re.search(r'IPv4 primary DNS: (\d+\.\d+\.\d+\.\d+)', output)
            
            if not (ip_match and gateway_match):
                print("✗ Failed to get IP settings")
                return False
            
            ip = ip_match.group(1)
            gateway = gateway_match.group(1)
            
            # Calculate CIDR from subnet mask
            if mask_match:
                mask = mask_match.group(1)
                cidr = sum([bin(int(x)).count('1') for x in mask.split('.')])
            else:
                cidr = 30
            
            # Configure interface
            subprocess.run(['sudo', 'ip', 'addr', 'flush', 'dev', 'wwan0'], 
                         stderr=subprocess.DEVNULL)
            subprocess.run(['sudo', 'ip', 'addr', 'add', f'{ip}/{cidr}', 'dev', 'wwan0'],
                         check=True)
            subprocess.run(['sudo', 'ip', 'link', 'set', 'wwan0', 'up'], check=True)
            
            # Remove ONLY old 4G routes (keep WiFi/Ethernet intact!)
            subprocess.run(['sudo', 'ip', 'route', 'del', 'default', 'dev', 'wwan0'], 
                         stderr=subprocess.DEVNULL)
            
            # Add 4G route with LOW metric (HIGH priority)
            # Metric 50 = higher priority than WiFi (usually 200-600)
            subprocess.run(['sudo', 'ip', 'route', 'add', 'default', 'via', gateway, 
                          'dev', 'wwan0', 'metric', '50'], check=True)
            
            # Set DNS
            if dns1_match:
                dns1 = dns1_match.group(1)
                with open('/tmp/resolv.conf.4g', 'w') as f:
                    f.write(f'nameserver {dns1}\n')
                    f.write('nameserver 8.8.8.8\n')
                subprocess.run(['sudo', 'cp', '/tmp/resolv.conf.4g', '/etc/resolv.conf'])
            
            print(f"✓ 4G configured: {ip} via {gateway} (metric 50)")
            return True
            
        except Exception as e:
            print(f"✗ 4G setup failed: {e}")
            return False
    
    def setup_wifi(self):
        """Setup WiFi connection - DOES NOT DELETE OTHER ROUTES"""
        print(f"Setting up WiFi connection to {self.wifi_ssid}...")
        
        try:
            # Bring interface up
            subprocess.run(['sudo', 'ip', 'link', 'set', 'wlan0', 'up'], check=True)
            time.sleep(2)
            
            # Check if already connected
            result = subprocess.run(['iwgetid', '-r'], capture_output=True, text=True)
            if result.returncode == 0 and self.wifi_ssid in result.stdout:
                print(f"✓ Already connected to {self.wifi_ssid}")
            else:
                # Connect to WiFi using wpa_cli
                if self.wifi_pass:
                    # Create wpa_supplicant config
                    wpa_conf = f"""
ctrl_interface=/var/run/wpa_supplicant
update_config=1

network={{
    ssid="{self.wifi_ssid}"
    psk="{self.wifi_pass}"
    key_mgmt=WPA-PSK
}}
"""
                    with open('/tmp/wpa_supplicant.conf', 'w') as f:
                        f.write(wpa_conf)
                    
                    # Start wpa_supplicant
                    subprocess.run(['sudo', 'pkill', '-f', 'wpa_supplicant.*wlan0'],
                                 stderr=subprocess.DEVNULL)
                    time.sleep(1)
                    subprocess.run(['sudo', 'wpa_supplicant', '-B', '-i', 'wlan0',
                                  '-c', '/tmp/wpa_supplicant.conf'],
                                 check=True)
                    time.sleep(5)
            
            # Get IP via DHCP
            subprocess.run(['sudo', 'pkill', '-f', 'dhclient.*wlan0'], 
                         stderr=subprocess.DEVNULL)
            time.sleep(1)
            
            result = subprocess.run(['sudo', 'dhclient', '-v', 'wlan0'],
                                  capture_output=True, text=True, timeout=30)
            
            time.sleep(2)
            
            # Check if got IP
            info = self.check_interface('wlan0')
            if info:
                print(f"✓ WiFi connected: {info['ip']}")
                
                # Remove ONLY old WiFi routes (keep 4G/Ethernet intact!)
                subprocess.run(['sudo', 'ip', 'route', 'del', 'default', 'dev', 'wlan0'], 
                             stderr=subprocess.DEVNULL)
                
                # Get gateway from interface routes
                result = subprocess.run(['ip', 'route', 'show', 'dev', 'wlan0'],
                                      capture_output=True, text=True)
                
                # Try to find gateway in output
                gateway = None
                for line in result.stdout.split('\n'):
                    match = re.search(r'via (\d+\.\d+\.\d+\.\d+)', line)
                    if match:
                        gateway = match.group(1)
                        break
                
                if not gateway:
                    # Calculate gateway from IP (assume .1 or .253/.254)
                    ip_parts = info['ip'].split('.')
                    gateway = f"{ip_parts[0]}.{ip_parts[1]}.{ip_parts[2]}.1"
                
                # Add WiFi route with HIGH metric (LOW priority)
                # Metric 200 = lower priority than 4G (50)
                subprocess.run(['sudo', 'ip', 'route', 'add', 'default', 'via',
                              gateway, 'dev', 'wlan0', 'metric', '200'])
                
                print(f"✓ WiFi route added via {gateway} (metric 200)")
                return True
            else:
                print("✗ Failed to get IP on WiFi")
                return False
                
        except Exception as e:
            print(f"✗ WiFi setup failed: {e}")
            return False
    
    def get_4g_operator(self):
        """Lấy tên nhà mạng từ AT commands"""
        try:
            import serial
            import serial.tools.list_ports
            
            # Tìm USB port
            ports = [p.device for p in serial.tools.list_ports.comports() if 'ttyUSB' in p.device]
            port = '/dev/ttyUSB2' if '/dev/ttyUSB2' in ports else (ports[0] if ports else None)
            
            if not port:
                return None
            
            # Kết nối serial
            ser = serial.Serial(port, 115200, timeout=2, rtscts=False, dsrdtr=False)
            ser.setDTR(False)
            ser.setRTS(False)
            time.sleep(0.3)
            ser.reset_input_buffer()
            
            # Gửi AT+COPS? để lấy operator
            ser.write(b"AT+COPS?\r\n")
            ser.flush()
            
            response = []
            start = time.time()
            while (time.time() - start) < 2:
                if ser.in_waiting > 0:
                    line = ser.readline().decode('utf-8', errors='ignore').strip()
                    if line:
                        response.append(line)
                    if 'OK' in line or 'ERROR' in line:
                        break
                time.sleep(0.05)
            
            ser.close()
            
            # Parse operator name từ response
            # Format: +COPS: 0,0,"Viettel Viettel",7
            for line in response:
                if '+COPS:' in line:
                    match = re.search(r'"([^"]+)"', line)
                    if match:
                        operator = match.group(1)
                        # Clean up duplicate names
                        if 'Viettel Viettel' in operator:
                            operator = 'Viettel'
                        elif 'Vinaphone Vinaphone' in operator:
                            operator = 'Vinaphone'
                        elif 'Mobifone Mobifone' in operator:
                            operator = 'Mobifone'
                        return operator
            
            return None
        except Exception as e:
            return None
    
    def get_interface_status(self, iface):
        """Get detailed status of an interface"""
        info = self.check_interface(iface)
        
        if not info:
            return {'status': 'unavailable'}
        
        # Has IP but no internet
        status_data = {
            'status': 'available',
            'ip': info['ip']
        }
        
        # Check if has default route
        try:
            result = subprocess.run(['ip', 'route', 'show', 'dev', iface, 'default'],
                                  capture_output=True, text=True, timeout=5)
            if result.stdout.strip():
                status_data['status'] = 'connected'
                
                # Get gateway
                match = re.search(r'via (\d+\.\d+\.\d+\.\d+)', result.stdout)
                if match:
                    status_data['gateway'] = match.group(1)
        except:
            pass
        
        return status_data
    
    def save_status(self):
        """Save current network status to file"""
        status = {
            '4g': self.get_interface_status('wwan0'),
            'wifi': self.get_interface_status('wlan0'),
            'ethernet': self.get_interface_status('eth0'),
            'active_interface': self.current_connection,
            'timestamp': int(time.time())
        }
        
        # Thêm operator name cho 4G nếu có
        if status['4g']['status'] in ['available', 'connected']:
            operator = self.get_4g_operator()
            if operator:
                status['4g']['operator'] = operator
                print(f"  ✓ Detected 4G operator: {operator}")
        
        try:
            with open(self.status_file, 'w') as f:
                json.dump(status, f, indent=2)
        except Exception as e:
            print(f"Error saving status: {e}")
    
    def auto_connect(self):
        """Connect with priority order - SMART ROUTING"""
        print(f"\n{'='*50}")
        print(f"Connection Manager - {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
        print(f"Priority: {self.priority.upper()}")
        print(f"{'='*50}\n")
        
        # Get order based on priority
        if self.priority == '4g':
            order = [('4G', 'wwan0', self.setup_4g), 
                    ('WiFi', 'wlan0', self.setup_wifi)]
        else:
            order = [('WiFi', 'wlan0', self.setup_wifi),
                    ('4G', 'wwan0', self.setup_4g)]
        
        connected = False
        
        for idx, (name, iface, setup_func) in enumerate(order, 1):
            print(f"{idx}. Checking {name} ({iface})...")
            
            info = self.check_interface(iface)
            
            if info and info['ip']:
                print(f"   ✓ {iface} has IP: {info['ip']}")
                
                # Test internet
                if self.ping_test():
                    print(f"   ✓ Internet OK via {name}")
                    self.current_connection = iface
                    connected = True
                    break
                else:
                    print(f"   ✗ No internet via {name}")
            else:
                print(f"   ✗ {iface} has no IP")
                print(f"   → Trying to setup {name}...")
                
                if setup_func():
                    time.sleep(2)
                    if self.ping_test():
                        print(f"   ✓ {name} connected with internet!")
                        self.current_connection = iface
                        connected = True
                        break
        
        # Check Ethernet as fallback
        print(f"\n3. Checking Ethernet (eth0)...")
        eth_info = self.check_interface('eth0')
        if eth_info:
            print(f"   ✓ eth0 has IP: {eth_info['ip']}")
            if not connected and self.ping_test():
                print(f"   ✓ Using Ethernet as fallback")
                self.current_connection = 'eth0'
                connected = True
        else:
            print(f"   ✗ eth0 has no IP")
        
        # Save status
        self.save_status()
        
        # Print result
        print()
        if connected:
            print(f"✓ Connected via {self.current_connection.upper()}")
        else:
            print("✗ No internet connection available")
        
        return connected

    def monitor(self):
        """Continuously monitor and auto-reconnect"""
        print("Starting connection monitor...")
        print("Press Ctrl+C to stop\n")
        
        while True:
            try:
                self.auto_connect()
                print(f"\nNext check in 30 seconds...\n")
                time.sleep(30)
            except KeyboardInterrupt:
                print("\nMonitor stopped")
                break
            except Exception as e:
                print(f"Error: {e}")
                time.sleep(10)

def main():
    manager = ConnectionManager()
    
    if len(sys.argv) < 2:
        print("Usage:")
        print("  python3 connection_manager.py once        # Connect once")
        print("  python3 connection_manager.py monitor     # Continuous monitoring")
        print("  python3 connection_manager.py status      # Show status")
        print("  python3 connection_manager.py priority <4g|wifi>  # Set priority")
        return
    
    command = sys.argv[1].lower()
    
    if command == 'once':
        manager.auto_connect()
    
    elif command == 'monitor':
        manager.monitor()
    
    elif command == 'status':
        # Show current connection
        wwan_info = manager.check_interface('wwan0')
        wifi_info = manager.check_interface('wlan0')
        eth_info = manager.check_interface('eth0')
        
        if wwan_info and manager.ping_test():
            print(f"Active: 4G - {wwan_info['ip']}")
        elif wifi_info and manager.ping_test():
            print(f"Active: WiFi - {wifi_info['ip']}")
        elif eth_info and manager.ping_test():
            print(f"Active: Ethernet - {eth_info['ip']}")
        else:
            print("Active: None")
    
    elif command == 'priority':
        if len(sys.argv) < 3:
            print(f"Current priority: {manager.priority}")
            print("Usage: python3 connection_manager.py priority <4g|wifi>")
        else:
            new_priority = sys.argv[2].lower()
            if new_priority in ['4g', 'wifi']:
                manager.save_config(new_priority)
                print(f"✓ Priority set to: {new_priority}")
            else:
                print("✗ Invalid priority. Use: 4g or wifi")
    
    else:
        print(f"Unknown command: {command}")

if __name__ == '__main__':
    main()
