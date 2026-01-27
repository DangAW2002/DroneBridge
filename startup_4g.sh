#!/bin/bash
# Startup script cho 4G - chạy bởi crontab @reboot

# Log file
LOGFILE="/home/pi/HBQ_server_drone/logs/4g_startup.log"
mkdir -p "$(dirname "$LOGFILE")"

# Log với timestamp
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a "$LOGFILE"
}

log "========================================="
log "Starting 4G Auto Enable"
log "========================================="

# Đợi system ổn định và USB devices enumerate
log "Waiting for system to stabilize..."
sleep 15

# Wait for USB serial devices
log "Checking for USB devices..."
for i in {1..30}; do
    if [ -e /dev/ttyUSB2 ] && [ -e /dev/ttyUSB3 ]; then
        log "✓ USB devices ready after ${i}s"
        break
    fi
    sleep 1
done

# Extra wait for QMI
sleep 5

# Chạy enable_4g_auto.py
log "Running enable_4g_auto.py..."
sudo /usr/bin/python3 /home/pi/HBQ_server_drone/Module_4G/enable_4g_auto.py >> "$LOGFILE" 2>&1

if [ $? -eq 0 ]; then
    log "✓ 4G auto enable completed successfully"
else
    log "✗ 4G auto enable failed with exit code $?"
fi

log "========================================="
log "Startup script completed"
log "========================================="
