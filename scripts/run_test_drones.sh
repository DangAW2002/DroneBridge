#!/bin/bash

# Change directory to DroneBridge root
cd "$(dirname "$0")/.."

echo "Cleaning up old instances..."
./scripts/stop_test_drones.sh 2>/dev/null || pkill -f dronebridge 2>/dev/null

echo "Building latest code..."
make build
if [ $? -ne 0 ]; then
    echo "Build failed. Exiting."
    exit 1
fi

BASE_LISTEN_PORT=15000
BASE_WEB_PORT=8090
DRONE_EXEC="./build/dronebridge"

# Check if executable exists
if [ ! -f "$DRONE_EXEC" ]; then
    echo "Error: $DRONE_EXEC not found. Please run 'make build' first."
    exit 1
fi

# Ask for number of drones if not provided
NUM_DRONES=$1
if [ -z "$NUM_DRONES" ]; then
    read -p "Enter number of drones to simulate (default 3): " NUM_DRONES
fi
# Default to 3 if empty
NUM_DRONES=${NUM_DRONES:-3}

PID_FILE="test_drones.pids"
: > "$PID_FILE" # Create/Clear PID file

echo "Starting $NUM_DRONES DroneBridge instances in TEST MODE..."
echo ""

# Pre-defined valid UUIDs
UUID_1="00000001-0000-0000-0000-000000000001"
UUID_2="00000002-0000-0000-0000-000000000002"
UUID_3="00000003-0000-0000-0000-000000000003"
UUID_4="00000004-0000-0000-0000-000000000004"
UUID_5="00000005-0000-0000-0000-000000000005"
UUID_6="00000006-0000-0000-0000-000000000006"
UUID_7="00000007-0000-0000-0000-000000000007"
UUID_8="00000008-0000-0000-0000-000000000008"
UUID_9="00000009-0000-0000-0000-000000000009"
UUID_10="0000000a-0000-0000-0000-00000000000a"
UUID_11="0000000b-0000-0000-0000-00000000000b"
UUID_12="0000000c-0000-0000-0000-00000000000c"
UUID_13="0000000d-0000-0000-0000-00000000000d"
UUID_14="0000000e-0000-0000-0000-00000000000e"
UUID_15="0000000f-0000-0000-0000-00000000000f"

for (( i=1; i<=NUM_DRONES; i++ ))
do
    LISTEN_PORT=$((BASE_LISTEN_PORT + i))
    WEB_PORT=$((BASE_WEB_PORT + i))
    
    # Get UUID for this index
    VAR_NAME="UUID_$i"
    CURRENT_UUID=${!VAR_NAME}
    
    if [ -z "$CURRENT_UUID" ]; then
        # Fallback generation
        CURRENT_UUID="00000000-0000-0000-test-00000000000$i"
    fi

    echo "[Instance $i] Launching..."
    echo "  UUID: $CURRENT_UUID"
    echo "  Web Port: $WEB_PORT"
    echo "  Listen Port: $LISTEN_PORT"
    
    # Run in background with nohup
    # Log to separate file per drone
    LOG_FILE="logs/drone_$i.log"
    mkdir -p logs
    
    nohup $DRONE_EXEC --test-mode -register -uuid="$CURRENT_UUID" -web-port=$WEB_PORT -listen-port=$LISTEN_PORT > "$LOG_FILE" 2>&1 &
    PID=$!
    
    echo $PID >> "$PID_FILE"
    echo "  PID: $PID"
    echo "  Log: $LOG_FILE"
    
    # Wait for discovery phase of current instance to finish
    echo "  Waiting 10s for discovery to complete..."
    sleep 10
done

echo ""
echo "All instances started."
echo "PIDs saved to $PID_FILE"
echo "To stop these specific instances, run: ./scripts/stop_test_drones.sh"
