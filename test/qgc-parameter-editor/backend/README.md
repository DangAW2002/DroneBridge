# MAVLink Parameter Bridge Backend

Go HTTP server that bridges frontend parameter editor with Pixhawk via MAVLink over Ethernet.

## Features

- Receives parameter set requests from frontend via HTTP REST API
- Sends MAVLink PARAM_SET messages to Pixhawk via UDP
- Monitors connection status via heartbeat
- Returns PARAM_VALUE acknowledgment to frontend

## Architecture

```
┌─────────────────┐     HTTP/REST     ┌──────────────────┐     MAVLink/UDP     ┌─────────────┐
│  React Frontend │ ◄──────────────►  │  Go Backend      │ ◄─────────────────► │  Pixhawk    │
│  (Port 3000)    │                   │  (Port 8080)     │                     │  (14550)    │
└─────────────────┘                   └──────────────────┘                     └─────────────┘
```

## API Endpoints

### POST /api/param/set
Set a parameter on the vehicle.

**Request Body:**
```json
{
  "paramName": "MAV_SYS_ID",
  "paramValue": 2,
  "paramType": "INT32"
}
```

**Response:**
```json
{
  "success": true,
  "message": "Parameter MAV_SYS_ID successfully set",
  "paramName": "MAV_SYS_ID",
  "newValue": 2
}
```

### GET /api/connection
Get current connection status to the vehicle.

**Response:**
```json
{
  "connected": true,
  "systemId": 1,
  "message": "Connected to Pixhawk (System ID: 1)"
}
```

### GET /api/health
Health check endpoint.

**Response:**
```json
{
  "status": "ok"
}
```

## Usage

### Build
```bash
cd backend
go build -o param-bridge .
```

### Run
```bash
# Default configuration (Pixhawk at 10.41.10.2:14550)
./param-bridge

# Custom configuration
./param-bridge -target 192.168.1.100:14550 -listen :14550 -port 8080 -timeout 5s
```

### Command Line Flags
| Flag | Default | Description |
|------|---------|-------------|
| `-target` | `10.41.10.2:14550` | Target Pixhawk UDP address |
| `-listen` | `:14550` | Local address to listen for MAVLink messages |
| `-port` | `8080` | HTTP server port |
| `-timeout` | `5s` | Timeout for waiting parameter response |

## Development

### Run with frontend
1. Start the backend:
   ```bash
   cd backend && ./param-bridge
   ```

2. Start the frontend (in another terminal):
   ```bash
   npm run dev
   ```

3. Open http://localhost:3000

### Parameter Types
The backend supports these PX4 parameter types:
- `INT32` - 32-bit signed integer
- `UINT32` - 32-bit unsigned integer  
- `INT16` - 16-bit signed integer
- `UINT16` - 16-bit unsigned integer
- `INT8` - 8-bit signed integer
- `UINT8` - 8-bit unsigned integer
- `FLOAT` / `REAL32` - 32-bit float

## Troubleshooting

### No connection to Pixhawk
1. Check network connectivity: `ping 10.41.10.2`
2. Verify Pixhawk is sending heartbeat on port 14550
3. Check firewall settings

### Parameter set timeout
1. Increase timeout: `./param-bridge -timeout 10s`
2. Check if parameter name is valid
3. Verify Pixhawk is not in flight mode that blocks parameter changes
