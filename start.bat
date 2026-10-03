@echo off
rem Switch the console to UTF-8, otherwise non-ASCII text in this file is garbled in a GBK terminal.
chcp 65001 >nul 2>&1
rem ARTEX supervisor launch script (Windows)
rem
rem Usage:
rem   start.bat                  Run in the foreground (Ctrl-C to stop)
rem   start.bat -addr :9000      Extra arguments are passed through to artex as-is
rem
rem It does one thing only: start artex.exe, and after the process exits, decide from the exit code whether to relaunch it.
rem
rem   0      Normal user stop     -> exit the loop
rem   75     Program requested restart     -> relaunch immediately (clicked "one-click update" or "roll back" in the UI)
rem   other  Crash             -> back off, then relaunch (1->2->4...up to 60 seconds)
rem
rem Downloading, SHA256 verification, and binary swapping are not here; they are all done by artex itself at startup
rem (the selfupdate package). The script stays dead simple; see the notes at the top of start.sh for details.

setlocal enabledelayedexpansion
cd /d "%~dp0"

set "BIN=artex.exe"
if not exist "%BIN%" (
	echo [artex] executable not found: %BIN% 1>&2
	exit /b 1
)

set "RESTART_CODE=75"
set "MAX_DELAY=60"
set /a delay=1

:loop
"%BIN%" %*
set "code=!ERRORLEVEL!"

if "!code!"=="0" (
	echo [artex] exited normally
	exit /b 0
)

if "!code!"=="%RESTART_CODE%" (
	rem Update/rollback is ready: after relaunch, artex completes the swap at startup.
	echo [artex] restart requested (applying the new version)...
	set /a delay=1
	goto loop
)

echo [artex] abnormal exit ^(code=!code!^), restarting in !delay!s 1>&2
rem timeout fails in a redirected console, so ping is used as a fallback (an N-second delay needs N+1 pings).
set /a pings=!delay!+1
ping -n !pings! 127.0.0.1 >nul 2>&1
set /a delay=!delay!*2
if !delay! gtr %MAX_DELAY% set /a delay=%MAX_DELAY%
goto loop
