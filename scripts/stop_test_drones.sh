#!/bin/bash

# Change directory to DroneBridge root
cd "$(dirname "$0")/.."

PID_FILE="test_drones.pids"

if [ ! -f "$PID_FILE" ]; then
    echo "Error: PID file $PID_FILE not found."
    echo "Are any test drones running?"
    exit 1
fi

echo "Stopping test drones listed in $PID_FILE..."

while IFS= read -r PID
do
    if [ -n "$PID" ]; then
        if kill -0 "$PID" 2>/dev/null; then
            echo "Stopping PID $PID..."
            kill "$PID"
        else
            echo "PID $PID not found/already stopped."
        fi
    fi
done < "$PID_FILE"

rm "$PID_FILE"
echo "Done."
