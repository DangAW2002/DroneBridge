#!/bin/bash

# Start QGC Parameter Editor with MAVLink Backend
# Usage: ./start.sh [pixhawk_ip]

PIXHAWK_IP="${1:-10.41.10.2}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "🚀 Starting QGC Parameter Editor..."
echo "   Pixhawk IP: $PIXHAWK_IP"

# Build backend if needed
if [ ! -f "$SCRIPT_DIR/backend/param-bridge" ]; then
    echo "📦 Building backend..."
    cd "$SCRIPT_DIR/backend" && go build -o param-bridge .
fi

# Start backend
echo "🔌 Starting MAVLink backend (port 8080)..."
cd "$SCRIPT_DIR/backend" && ./param-bridge -target "$PIXHAWK_IP:14550" &
BACKEND_PID=$!

# Wait for backend to start
sleep 1

# Start frontend
echo "🌐 Starting frontend (port 3000)..."
cd "$SCRIPT_DIR" && npm run dev &
FRONTEND_PID=$!

# Trap Ctrl+C to cleanup
trap "echo '🛑 Stopping...'; kill $BACKEND_PID $FRONTEND_PID 2>/dev/null; exit" INT TERM

echo ""
echo "✅ Services started!"
echo "   Frontend: http://localhost:3000"
echo "   Backend:  http://localhost:8080"
echo ""
echo "Press Ctrl+C to stop all services"

# Wait for processes
wait
