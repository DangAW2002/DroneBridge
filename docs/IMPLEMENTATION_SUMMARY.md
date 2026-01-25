# Implementation Summary: Pixhawk Connection Requirement

## What Was Changed

DroneBridge now enforces connecting to Pixhawk **before** authenticating with the server.

## Key Changes

### 1. Configuration (config/config.go & config.yaml)

**Added two new parameters** to `EthernetConfig`:

```go
AllowMissingPixhawk         bool   // DEBUG: Allow auth without Pixhawk (for testing)
PixhawkConnectionTimeout    int    // Timeout in seconds to wait for connection
```

**In config.yaml**:
```yaml
ethernet:
  allow_missing_pixhawk: false           # Require Pixhawk connection (production)
  pixhawk_connection_timeout: 30         # Wait 30 seconds for heartbeat
```

### 2. Forwarder (forwarder/forwarder.go)

**Added connection tracking**:
```go
pixhawkConnected chan struct{}  // Signal when first heartbeat received
pixhawkOnce      sync.Once      // Ensure signal fires only once
```

**New public methods**:
```go
// Wait for Pixhawk connection with timeout
WaitForPixhawkConnection(timeout time.Duration) bool

// Set auth client after connection confirmed
SetAuthClient(authClient *auth.Client)
```

**Logic in receiveAndForward()**:
- On first HEARTBEAT message, close `pixhawkConnected` channel
- Logs: `[PIXHAWK_CONNECTED] ✅ First heartbeat received from Pixhawk (SysID: X)`

### 3. Main Startup Sequence (main.go)

**Changed order from**:
```
1. Auth → 2. Forwarder
```

**To**:
```
1. Forwarder (listening)
   ↓
2. Wait for Pixhawk heartbeat
   ↓
3. Check config (fail if not connected AND AllowMissingPixhawk=false)
   ↓
4. Authenticate
   ↓
5. Start web server
```

**Code flow**:
```go
// Step 1: Create and start forwarder FIRST (just listening)
fwd, err := forwarder.New(cfg, nil)  // nil authClient
fwd.Start()

// Step 2: Wait for Pixhawk
pixhawkConnected := fwd.WaitForPixhawkConnection(timeout)
if !pixhawkConnected && !cfg.Ethernet.AllowMissingPixhawk {
    logger.Fatal("❌ Pixhawk connection failed")
}

// Step 3: Now authenticate
authClient.Start()
fwd.SetAuthClient(authClient)  // Wire up callbacks

// Step 4: Continue with web server
web.StartServer(...)
```

## Behavior

### Production (default)
```yaml
allow_missing_pixhawk: false
pixhawk_connection_timeout: 30
```

✅ **Success**: Pixhawk connects within 30s
```
[PIXHAWK_CONNECTED] ✅ First heartbeat received from Pixhawk (SysID: 1)
[STARTUP] ✈️  Now proceeding with server authentication...
```

❌ **Failure**: No Pixhawk within 30s
```
[STARTUP] ❌ Pixhawk connection failed. Set 'allow_missing_pixhawk: true' in config to skip this requirement.
```

### Debug Mode
```yaml
allow_missing_pixhawk: true
pixhawk_connection_timeout: 5
```

⏭️ **Skip Check**: Always proceed even if no Pixhawk
```
[STARTUP] ⚠️  Pixhawk connection timeout, but AllowMissingPixhawk=true, continuing...
[STARTUP] ⚠️  Running in DEBUG mode without actual Pixhawk connection!
```

## Logging

### First Heartbeat (Critical Event)
```
[PIXHAWK_CONNECTED] ✅ First heartbeat received from Pixhawk (SysID: X)
[SYSID] Detected Pixhawk System ID: X (using for MAVLink operations)
```

### Startup Flow
```
[STARTUP] Starting forwarder to listen for Pixhawk...
[STARTUP] ⏳ Waiting for Pixhawk heartbeat... (timeout: 30s)
[STARTUP] ✅ Pixhawk connected successfully!
[STARTUP] Pixhawk System ID: 1
[STARTUP] ✈️  Now proceeding with server authentication...
```

## Files Modified

1. **config/config.go**
   - Added `AllowMissingPixhawk` and `PixhawkConnectionTimeout` to `EthernetConfig`
   - Added default timeout = 30 seconds

2. **config.yaml**
   - Added `allow_missing_pixhawk: false` (production default)
   - Added `pixhawk_connection_timeout: 30`

3. **forwarder/forwarder.go**
   - Added `pixhawkConnected` and `pixhawkOnce` fields
   - Added `WaitForPixhawkConnection()` method
   - Added `SetAuthClient()` method
   - Signal on first heartbeat in `receiveAndForward()`

4. **main.go**
   - Reordered startup sequence: Forwarder → Wait Pixhawk → Auth
   - Added config checks for `AllowMissingPixhawk`
   - Use `SetAuthClient()` to wire up after Pixhawk connected

5. **config-debug.yaml** (NEW)
   - Debug configuration with `allow_missing_pixhawk: true`
   - For testing without actual Pixhawk

## Files Created

1. **docs/PIXHAWK_CONNECTION_REQUIREMENT.md**
   - Complete documentation of this feature
   - Troubleshooting guide
   - Configuration examples

2. **config-debug.yaml**
   - Debug mode configuration
   - For development testing

## Testing

### Production Test
```bash
./dronebridge -config config.yaml
# Must have Pixhawk connected within 30 seconds
```

### Debug Test
```bash
./dronebridge -config config-debug.yaml
# Works without Pixhawk (5 second wait then continues)
```

### Custom Timeout
```yaml
# config.yaml
ethernet:
  pixhawk_connection_timeout: 60  # Wait 1 minute instead of 30 seconds
```

## Benefits

✅ **Reliability**: Guarantees Pixhawk is connected before proceeding
✅ **Real System ID**: Detects and uses actual drone's system ID
✅ **Testing**: Debug mode allows testing without hardware
✅ **Clear Failure**: Users know exactly what went wrong
✅ **Logging**: Detailed logs show connection status

## Backward Compatibility

- **Config files without new parameters**: Use defaults (30 second timeout, required connection)
- **Existing code**: No breaking changes to forwarder or auth APIs
- **Web API**: Still returns actual system ID via `/api/connection`

## Related Features

- **Dynamic System ID**: Uses actual Pixhawk sys_id detected from heartbeat (see: `DYNAMIC_SYSTEM_ID.md`)
- **Parameter Editor**: Uses detected system ID for PARAM_REQUEST_LIST
- **Web Status**: Shows `Connected (ID: X)` on frontend
