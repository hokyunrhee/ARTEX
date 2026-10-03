@echo off
rem Use UTF-8 so Unicode output and user-provided content display correctly.
chcp 65001 >nul 2>&1
rem ARTEX supervisor startup script (Windows).
rem
rem Usage:
rem   start.bat                  Run in the foreground (Ctrl-C to stop).
rem   start.bat -addr :9000      Pass additional arguments directly to artex.
rem
rem Start artex.exe and use its exit code to decide whether to restart it.
rem
rem   0      Normal user-requested stop -> leave the loop.
rem   75     Restart requested          -> restart immediately (UI update or rollback).
rem   Other  Crash                      -> restart with backoff (1, 2, 4, up to 60 seconds).
rem
rem artex handles downloads, SHA256 verification, and binary replacement at startup
rem through selfupdate. Keep this script simple; see the notes at the top of start.sh.

setlocal enabledelayedexpansion
cd /d "%~dp0"

set "BIN=artex.exe"
if not exist "%BIN%" (
	echo [artex] Executable not found: %BIN% 1>&2
	exit /b 1
)

set "RESTART_CODE=75"
set "MAX_DELAY=60"
set /a delay=1

:loop
"%BIN%" %*
set "code=!ERRORLEVEL!"

if "!code!"=="0" (
	echo [artex] Exited normally
	exit /b 0
)

if "!code!"=="%RESTART_CODE%" (
	rem Update/rollback is ready; artex applies it on startup.
	echo [artex] Restart requested ^(applying the new version^)...
	set /a delay=1
	goto loop
)

echo [artex] Unexpected exit ^(code=!code!^); restarting in !delay!s 1>&2
rem timeout fails with redirected consoles; fall back to ping (N seconds needs N+1 attempts).
set /a pings=!delay!+1
ping -n !pings! 127.0.0.1 >nul 2>&1
set /a delay=!delay!*2
if !delay! gtr %MAX_DELAY% set /a delay=%MAX_DELAY%
goto loop
