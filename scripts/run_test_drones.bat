@echo off
setlocal enabledelayedexpansion

REM Change directory to DroneBridge root (parent of script folder)
cd /d "%~dp0.."

echo Cleaning up old instances...
taskkill /F /IM dronebridge.exe /T >nul 2>&1

echo Building latest code...
go build -o build\dronebridge.exe .
if %ERRORLEVEL% NEQ 0 (
    echo Build failed. Exiting.
    pause
    exit /b 1
)

REM Base configuration
set "BASE_LISTEN_PORT=14541"
set "BASE_WEB_PORT=8090"
set "DRONE_EXEC=build\dronebridge.exe"

REM Check if executable exists
if not exist "%DRONE_EXEC%" (
    echo Error: %DRONE_EXEC% not found. Please build the project first (e.g., go build -o build\dronebridge.exe .).
    exit /b 1
)

REM Ask for number of drones if not provided
set "NUM_DRONES=%1"
if "%NUM_DRONES%"=="" (
    set /p "NUM_DRONES=Enter number of drones to simulate (default 3): "
)
if "%NUM_DRONES%"=="" set "NUM_DRONES=3"

echo Preparing to start %NUM_DRONES% DroneBridge instances in Windows Terminal...

REM Pre-defined valid UUIDs
set "UUID_1=00000000-0000-0000-test-000000000001"
set "UUID_2=00000000-0000-0000-test-000000000002"
set "UUID_3=00000000-0000-0000-test-000000000003"
set "UUID_4=00000000-0000-0000-test-000000000004"
set "UUID_5=00000000-0000-0000-test-000000000005"
set "UUID_6=00000000-0000-0000-test-000000000006"
set "UUID_7=00000000-0000-0000-test-000000000007"
set "UUID_8=00000000-0000-0000-test-000000000008"

set "WT_CMD="

for /L %%i in (1,1,%NUM_DRONES%) do (
    set /a "LISTEN_PORT=BASE_LISTEN_PORT + %%i"
    set /a "WEB_PORT=BASE_WEB_PORT + %%i"
    
    REM Get UUID for this index
    set "CURRENT_UUID=!UUID_%%i!"
    if "!CURRENT_UUID!"=="" (
        set "CURRENT_UUID=00000000-0000-0000-test-0000000000%%i"
    )

    echo [Instance %%i] Config:
    echo   UUID: !CURRENT_UUID!
    echo   Ports: Web=!WEB_PORT!, Listen=!LISTEN_PORT!

    REM Construct command for this drone
    REM Using cmd /k to keep window open
    set "DRONE_CMD=cmd /k \"%DRONE_EXEC% --test-mode -register -uuid=!CURRENT_UUID! -web-port=!WEB_PORT! -listen-port=!LISTEN_PORT!\""
    
    if %%i EQU 1 (
        REM First tab starts the wt instance
        set "WT_CMD=wt -p "Command Prompt" --title "Drone !CURRENT_UUID!" !DRONE_CMD!"
    ) else (
        REM Subsequent tabs
        set "WT_CMD=!WT_CMD! ; new-tab -p "Command Prompt" --title "Drone !CURRENT_UUID!" !DRONE_CMD!"
    )

    REM Wait 10s between instances to allow clean discovery (except last one)
    if %%i LSS %NUM_DRONES% (
        echo   Waiting 10s for discovery to complete...
        timeout /t 10 /nobreak >nul
    )
)

echo.
echo Executing Windows Terminal command...
REM Execute the constructed command
%WT_CMD%
