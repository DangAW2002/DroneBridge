# Dynamic System ID Usage in DroneBridge

## Problem with Hardcoded System IDs

Previously, DroneBridge had hardcoded `OutSystemID: 1` in multiple places. This was problematic because:
- Not all Pixhawk/PX4 systems use system ID = 1
- System ID should be detected from the actual connected drone
- Hardcoding breaks compatibility with drones using different system IDs

## Solution: Dynamic System ID Detection

### How It Works

1. **Forwarder receives heartbeat from Pixhawk**
   - Each MAVLink heartbeat contains the sender's system ID
   - File: `forwarder/forwarder.go` - `receiveAndForward()`

2. **System ID is captured and stored**
   - When first heartbeat arrives: `web.HandleHeartbeat(sysID)` is called
   - Web bridge stores: `bridge.pixhawkSysID = sysID`
   - File: `web/server.go` - `HandleHeartbeat()`

3. **System ID is made available for all operations**
   - Export function: `web.GetPixhawkSystemID()`
   - Returns: The actual Pixhawk system ID or fallback to 1 if not connected
   - File: `web/server.go` - `GetPixhawkSystemID()`

4. **Web server uses the actual system ID for parameter operations**
   - Parameter requests use `bridge.GetSystemID()` 
   - File: `web/server.go` - `RequestParameterList()`

### Usage in Code

#### In forwarder.go
```go
// Get the actual Pixhawk system ID
actualSysID := web.GetPixhawkSystemID()
logger.Debug("[SYSID] Detected Pixhawk System ID: %d", actualSysID)

// For PARAM_REQUEST_LIST:
msg := &common.MessageParamRequestList{
    TargetSystem: actualSysID,  // Use dynamic ID instead of hardcoded 1
    TargetComponent: 1,
}
```

#### In web/server.go
```go
// Inside RequestParameterList()
sysID := b.GetSystemID()  // Gets the actual Pixhawk system ID

msg := &common.MessageParamRequestList{
    TargetSystem: sysID,  // Dynamic system ID
    TargetComponent: 1,
}
```

## Flow Diagram

```
┌─────────────────────────────────────────────────────┐
│ Pixhawk sends HEARTBEAT (SysID=N)                   │
└──────────────────┬──────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────┐
│ forwarder/forwarder.go - receiveAndForward()        │
│ - Receives: sysID = N from heartbeat                │
│ - Calls: web.HandleHeartbeat(sysID)                 │
└──────────────────┬──────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────┐
│ web/server.go - HandleHeartbeat()                   │
│ - Stores: bridge.pixhawkSysID = N                   │
│ - Status: bridge.connected = true                   │
└──────────────────┬──────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────┐
│ Available for all operations:                       │
│ web.GetPixhawkSystemID() → returns N                │
│                                                     │
│ Used by:                                            │
│ - Parameter requests in web/server.go               │
│ - Forwarder logging in forwarder/forwarder.go       │
│ - Parameters editor in params.html                  │
└─────────────────────────────────────────────────────┘
```

## Important Notes

1. **Fallback Value**: Returns 1 if bridge not initialized
   - This is the standard PX4 default system ID
   - Ensures compatibility if called before connection

2. **Thread Safety**: 
   - `GetPixhawkSystemID()` is thread-safe with mutex locks
   - Used by multiple goroutines (forwarder, web server)

3. **Connection Timing**:
   - System ID is only valid after first heartbeat received
   - Before connection: returns 0 from bridge
   - Our exported function returns fallback 1 in that case

4. **Params HTML Integration**:
   - Frontend shows `Connected (ID: {systemId})` via `/api/connection` endpoint
   - Backend returns `GetPixhawkSystemID()` in response
   - UI updates show the actual connected system ID

## Testing

To verify system ID detection:

1. **Check logs for system ID capture**:
   ```
   [SYSID] Detected Pixhawk System ID: X (using for MAVLink operations)
   [PIXHAWK] Heartbeat: Type=?, Mode=?, Status=?
   ```

2. **Check web UI connection status**:
   - Open DroneBridge web interface
   - Should show: `Connected (ID: X)` after Pixhawk connects

3. **Verify parameter operations use correct system ID**:
   - Request parameters from web UI
   - Check forwarder logs for parameter requests with correct target system ID

## Files Modified

- `web/server.go`: Added `GetPixhawkSystemID()` exported function
- `forwarder/forwarder.go`: 
  - Added `getPixhawkSystemID()` helper function
  - Updated comments on hardcoded OutSystemID
  - Added system ID logging on heartbeat
