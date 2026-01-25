# DroneBridge Startup Flow

## New Startup Sequence (v2)

```
┌──────────────────────────────────────────────────────────────────┐
│  DroneBridge Started                                             │
└──────────────────────────────┬───────────────────────────────────┘
                               │
                               ▼
                    ┌──────────────────────┐
                    │  Load Configuration  │
                    │  (config.yaml)       │
                    └──────────┬───────────┘
                               │
                               ▼
        ┌──────────────────────────────────────────────┐
        │  Create Auth Client                          │
        │  (No Start Yet - needed for Forwarder)       │
        └──────────────────┬───────────────────────────┘
                           │
                           ▼
        ┌──────────────────────────────────────────────┐
        │  🎯 STEP 1: Create & Start Forwarder         │
        │                                              │
        │  fwd.New(cfg, nil)  ← Pass nil for authClient
        │  fwd.Start()        ← Begin listening       │
        └──────────────────┬───────────────────────────┘
                           │
                           ▼
        ┌──────────────────────────────────────────────┐
        │  🎯 STEP 2: Wait for Pixhawk Connection      │
        │                                              │
        │  fwd.WaitForPixhawkConnection(30 seconds)   │
        │                                              │
        │  ⏳ Listening for HEARTBEAT on port 14542    │
        └──────────────────┬───────────────────────────┘
                           │
                 ┌─────────┴─────────┐
                 │                   │
                 ▼                   ▼
        ✅ HEARTBEAT         ❌ TIMEOUT
        RECEIVED!            AFTER 30s
                 │                   │
                 │                   ▼
                 │        ┌──────────────────────┐
                 │        │ Check Config:        │
                 │        │ allow_missing_      │
                 │        │ pixhawk?            │
                 │        └────┬────────────────┘
                 │             │
                 │      ┌──────┴──────┐
                 │      │             │
                 │      ▼             ▼
                 │   TRUE:        FALSE:
                 │   ⚠️ WARN       ❌ FATAL
                 │   Continue     EXIT CODE 1
                 │      │             │
                 │      └──────┬──────┘
                 │             │
                 ▼             ▼
        ┌──────────────────────────────────────┐
        │  Extract System ID from HEARTBEAT    │
        │  web.GetPixhawkSystemID() → SysID    │
        └──────────────────┬───────────────────┘
                           │
                           ▼
        ┌──────────────────────────────────────┐
        │  🎯 STEP 3: Authenticate             │
        │                                      │
        │  authClient.Start()                 │
        │  Wait for auth (max 10 seconds)     │
        └──────────────────┬───────────────────┘
                           │
                           ▼
        ┌──────────────────────────────────────┐
        │  🎯 STEP 4: Start Web Server         │
        │                                      │
        │  web.StartServer(port, authClient)  │
        │  web.InitMAVLinkBridge(...)         │
        └──────────────────┬───────────────────┘
                           │
                           ▼
        ┌──────────────────────────────────────┐
        │  ⚡ Wire Auth Client to Forwarder     │
        │                                      │
        │  fwd.SetAuthClient(authClient)      │
        │  (re-register callbacks)            │
        └──────────────────┬───────────────────┘
                           │
                           ▼
        ┌──────────────────────────────────────┐
        │  ✅ READY TO OPERATE                 │
        │                                      │
        │  • Forwarding MAVLink packets        │
        │  • Authenticating with server       │
        │  • Web server running on :8080      │
        │  • Parameter editor active          │
        └──────────────────────────────────────┘
```

## State Diagrams

### Forwarder States

```
         ┌─────────────────────┐
         │   CREATED           │
         │  (Not Started)      │
         └──────────┬──────────┘
                    │ New()
                    ▼
         ┌─────────────────────┐
         │   LISTENING         │
         │  fwd.Start()        │ ← Forwarder.Start()
         │                     │
         │ - Waiting for HB    │
         │ - No auth yet       │
         └──────────┬──────────┘
                    │ [First HEARTBEAT received]
                    ▼
         ┌─────────────────────┐
         │  PIXHAWK_CONNECTED  │
         │                     │
         │ - SysID Captured    │
         │ - Ready to auth     │
         └──────────┬──────────┘
                    │ SetAuthClient()
                    ▼
         ┌─────────────────────┐
         │   AUTHENTICATED     │
         │  & FORWARDING       │
         │                     │
         │ - Auth active       │
         │ - Forwarding data   │
         └─────────────────────┘
```

### Config Decision Tree

```
                START
                  │
                  ▼
        ┌─────────────────────┐
        │ allow_missing_      │
        │ pixhawk?            │
        └────┬────────────────┘
             │
       ┌─────┴─────┐
       │           │
       ▼           ▼
     TRUE        FALSE
       │           │
       │           ▼
       │   ┌──────────────┐
       │   │ Pixhawk      │
       │   │ connected?   │
       │   └────┬─────────┘
       │        │
       │   ┌────┴────┐
       │   │         │
       │   ▼         ▼
       │  YES       NO
       │   │        │
       │   │        ▼
       │   │    ❌ FATAL EXIT
       │   │    "Pixhawk connection failed"
       │   │
       └───┴─────────┐
                     │
                     ▼
            ✅ CONTINUE STARTUP
```

## Log Timeline

### Successful Connection

```
T+0s   [STARTUP] Starting forwarder to listen for Pixhawk...
T+0s   [NETWORK] UDP Server enabled on 0.0.0.0:14542
T+0s   [STARTUP] ⏳ Waiting for Pixhawk heartbeat... (timeout: 30s)
T+1s   [RX] HEARTBEAT (SysID: 1, Seq: 0)
T+1s   [PIXHAWK_CONNECTED] ✅ First heartbeat received from Pixhawk (SysID: 1)
T+1s   [WEB] Connected to Pixhawk (System ID: 1)
T+1s   [SYSID] Detected Pixhawk System ID: 1 (using for MAVLink operations)
T+1s   [STARTUP] ✅ Pixhawk connected successfully!
T+1s   [STARTUP] Pixhawk System ID: 1
T+1s   [STARTUP] ✈️  Now proceeding with server authentication...
T+1s   [AUTH] Authenticating with server...
T+2s   [AUTH] ✅ Authentication successful
T+2s   [WEB] Starting web server on port 8080
T+2s   [STARTUP] ✅ DroneBridge ready!
```

### Timeout (with AllowMissingPixhawk=false)

```
T+0s   [STARTUP] Starting forwarder to listen for Pixhawk...
T+0s   [STARTUP] ⏳ Waiting for Pixhawk heartbeat... (timeout: 30s)
...    (30 seconds pass, no heartbeat)
T+30s  [STARTUP] ❌ Pixhawk connection failed. Set 'allow_missing_pixhawk: true' in config to skip this requirement.
       fatal: exit code 1
```

### Timeout (with AllowMissingPixhawk=true)

```
T+0s   [STARTUP] Starting forwarder to listen for Pixhawk...
T+0s   [STARTUP] ⏳ Waiting for Pixhawk heartbeat... (timeout: 5s)
...    (5 seconds pass, no heartbeat)
T+5s   [STARTUP] ⚠️  Pixhawk connection timeout, but AllowMissingPixhawk=true, continuing...
T+5s   [STARTUP] ⚠️  Running in DEBUG mode without actual Pixhawk connection!
T+5s   [STARTUP] ✈️  Now proceeding with server authentication...
T+5s   [AUTH] Authenticating with server...
T+6s   [AUTH] ✅ Authentication successful
T+6s   [WEB] Starting web server on port 8080
T+6s   [STARTUP] ✅ DroneBridge ready!
```

## System ID Flow

```
Pixhawk              Forwarder         Web Bridge         Client
  │                    │                  │                 │
  │─────HEARTBEAT──────>                  │                 │
  │      (SysID: 1)     │                  │                 │
  │                     │──HandleHB(1)───>│                 │
  │                     │     ✅           │ store SysID=1   │
  │                     │                  │                 │
  │                     │  WaitPixhawk()   │                 │
  │                     │      signal ────>│                 │
  │                     │     (open ch)    │                 │
  │                     │                  │                 │
  │                     │                  │  GetPixhawkSystemID()
  │                     │                  │      ◄─────────return 1
  │                     │                  │                 │
  │                     │              SetAuthClient()       │
  │                     │      ◄────────wire callbacks───    │
  │                     │                  │                 │
  │ Forward Messages    │  Authenticate    │                 │
  │─────────────────────>  with server     │                 │
```

## Configuration Impact

### Production (Strict)
```yaml
allow_missing_pixhawk: false
pixhawk_connection_timeout: 30
```
- **Startup blocked** until Pixhawk connects
- **System ID** from actual drone
- **Guaranteed** hardware connection

### Debug (Lenient)
```yaml
allow_missing_pixhawk: true
pixhawk_connection_timeout: 5
```
- **Startup continues** even without Pixhawk
- **System ID** defaults to 1
- **Testing** without hardware possible
