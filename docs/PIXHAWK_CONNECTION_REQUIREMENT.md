# Pixhawk Connection Requirement

## Overview

DroneBridge now enforces a connection to the Pixhawk before authenticating with the server. This ensures:
- ✅ Actual Pixhawk hardware is connected and functional
- ✅ System ID is detected from the real drone
- ✅ All parameters are loaded from the actual device
- ✅ Proper initialization sequence is followed

## Startup Sequence

```
[1] Start Forwarder
    ├─ Listen for Pixhawk on configured port
    └─ Waiting for HEARTBEAT...

[2] Wait for Pixhawk Connection
    ├─ Timeout: configured value (default 30 seconds)
    ├─ On success: Extract System ID from HEARTBEAT
    └─ On failure: Check config...

[3] Authenticate with Server
    ├─ Only if Pixhawk connected OR allow_missing_pixhawk=true
    └─ Start forwarding MAVLink messages

[4] Start Web Server
    └─ Parameter editor, status page, etc.
```

## Configuration

### Default Behavior (Production)

```yaml
ethernet:
  allow_missing_pixhawk: false           # ENFORCED - Pixhawk must connect
  pixhawk_connection_timeout: 30         # Wait 30 seconds for heartbeat
```

**Result**: Application will **FAIL** if no Pixhawk heartbeat within 30 seconds

### Debug Mode (Testing)

```yaml
ethernet:
  allow_missing_pixhawk: true            # ALLOW - Skip Pixhawk check for testing
  pixhawk_connection_timeout: 5          # Short timeout (won't block long)
```

**Result**: Application will **continue** even if no Pixhawk, using fallback System ID = 1

## Usage

### Production Deployment

```bash
# Use default config - will wait for Pixhawk
./dronebridge -config config.yaml

# Logs:
# [STARTUP] ⏳ Waiting for Pixhawk heartbeat... (timeout: 30s)
# [PIXHAWK_CONNECTED] ✅ First heartbeat received from Pixhawk (SysID: 1)
# [STARTUP] ✈️  Now proceeding with server authentication...
```

### Testing Without Pixhawk

Edit `config.yaml`:
```yaml
ethernet:
  allow_missing_pixhawk: true    # Enable debug mode
```

Then run:
```bash
./dronebridge -config config.yaml

# Logs:
# [STARTUP] ⏳ Waiting for Pixhawk heartbeat... (timeout: 30s)
# [STARTUP] ⚠️  Pixhawk connection timeout, but AllowMissingPixhawk=true, continuing...
# [STARTUP] ⚠️  Running in DEBUG mode without actual Pixhawk connection!
# [STARTUP] ✈️  Now proceeding with server authentication...
```

## Key Functions

### In forwarder/forwarder.go

#### WaitForPixhawkConnection()
```go
// Wait for Pixhawk connection with timeout
connected := fwd.WaitForPixhawkConnection(time.Duration(cfg.Ethernet.PixhawkConnectionTimeout) * time.Second)
if connected {
    logger.Info("Pixhawk connected!")
    sysID := web.GetPixhawkSystemID()
}
```

#### SetAuthClient()
```go
// Set auth client after Pixhawk connection confirmed
fwd.SetAuthClient(authClient)
```

### In main.go

```go
// Step 1: Create and start forwarder (listening phase)
fwd, err := forwarder.New(cfg, nil)
fwd.Start()

// Step 2: Wait for Pixhawk heartbeat
pixhawkConnected := fwd.WaitForPixhawkConnection(timeout)

// Step 3: Proceed with auth
authClient.Start()

// Step 4: Set auth client on forwarder
fwd.SetAuthClient(authClient)
```

## Log Output

### Successful Connection

```
[STARTUP] ⏳ Waiting for Pixhawk heartbeat... (timeout: 30s)
[PIXHAWK_CONNECTED] ✅ First heartbeat received from Pixhawk (SysID: 1)
[STARTUP] ✈️  Now proceeding with server authentication...
```

### Timeout with AllowMissingPixhawk=false

```
[STARTUP] ⏳ Waiting for Pixhawk heartbeat... (timeout: 30s)
[STARTUP] ❌ Pixhawk connection failed. Set 'allow_missing_pixhawk: true' in config to skip this requirement.
fatal: exit code 1
```

### Timeout with AllowMissingPixhawk=true

```
[STARTUP] ⏳ Waiting for Pixhawk heartbeat... (timeout: 30s)
[STARTUP] ⚠️  Pixhawk connection timeout, but AllowMissingPixhawk=true, continuing...
[STARTUP] ⚠️  Running in DEBUG mode without actual Pixhawk connection!
[STARTUP] ✈️  Now proceeding with server authentication...
```

## Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `allow_missing_pixhawk` | bool | false | Allow auth without Pixhawk (DEBUG only) |
| `pixhawk_connection_timeout` | int | 30 | Timeout in seconds to wait for heartbeat |

## Troubleshooting

### "Pixhawk connection failed" - But my Pixhawk IS connected!

**Check**:
1. Pixhawk is powered on
2. USB/Serial cable is properly connected
3. Network interface configuration (`ethernet.interface`, `ethernet.local_ip`)
4. Firewall/permissions for port 14542

**Solution**:
```bash
# Increase timeout temporarily for debugging
# Edit config.yaml:
pixhawk_connection_timeout: 60  # 60 seconds

# Check logs for more details
./dronebridge -config config.yaml -log debug
```

### "❌ Pixhawk connection failed. Set 'allow_missing_pixhawk: true'..."

This is **intentional** - the application requires a connected Pixhawk in production.

**To test without Pixhawk**:
```yaml
# config.yaml
ethernet:
  allow_missing_pixhawk: true
```

### Pixhawk connects but still says "failed"?

Check System ID detection:
```bash
# In logs, look for:
# [PIXHAWK_CONNECTED] ✅ First heartbeat received from Pixhawk (SysID: X)
# [SYSID] Detected Pixhawk System ID: X (using for MAVLink operations)

# If these don't appear, check:
1. Pixhawk is actually sending heartbeats
2. Forwarder is receiving messages (check [RX] logs)
3. No firewall blocking UDP port 14542
```

## Integration with params.html

The web parameter editor uses the detected system ID:

```javascript
// params.html
fetch('/api/connection')  // Gets: { connected: true, systemId: 1 }
// Shows: "Connected (ID: 1)"

// Parameter operations use this systemId:
fetch('/api/param/request-list', {method: 'POST'})
// Sends PARAM_REQUEST_LIST with TargetSystem = 1
```

## Performance Impact

- **Startup delay**: +0 to 30 seconds (depending on connection time)
- **Memory**: Negligible (one channel for connection signal)
- **CPU**: Negligible (minimal monitoring overhead)

## Related Files

- `config.yaml` - Configuration with new parameters
- `config/config.go` - EthernetConfig structure
- `forwarder/forwarder.go` - Connection signaling logic
- `main.go` - Startup sequence orchestration
- `web/server.go` - System ID exposure via API
