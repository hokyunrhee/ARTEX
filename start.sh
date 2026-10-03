#!/bin/sh
# ARTEX supervisor startup script (Linux / macOS / Docker ENTRYPOINT).
#
# Usage:
#   ./start.sh                       Run in the foreground (Ctrl-C to stop).
#   nohup ./start.sh >artex.log 2>&1 &   Run in the background.
#   ./start.sh -addr :9000           Pass additional arguments directly to artex.
#
# Start artex and use its exit code to decide whether to restart it.
#
#   0      Normal user-requested stop -> leave the loop.
#   75     Restart requested          -> restart immediately (UI update or rollback).
#   Other  Crash                      -> restart with backoff (1, 2, 4, up to 60 seconds).
#
# Downloads, SHA256 verification, and binary replacement are deliberately handled in Go.
# Duplicating these critical steps in sh and bat risks installing a broken executable
# that this script would repeatedly restart until someone repairs it manually.
# artex performs verification and replacement at startup through selfupdate; keep this script simple.
set -u

cd "$(dirname "$0")" || exit 1

BIN=./artex
[ -x "$BIN" ] || { echo "[artex] Executable not found: $BIN" >&2; exit 1; }

RESTART_CODE=75
MAX_DELAY=60

child=0
stopping=0

# Forward stop signals to artex.
#
# Docker sends SIGTERM only to PID 1 (this script), not to its child process.
# Without forwarding, artex cannot shut down gracefully and is killed with SIGKILL
# after 10 seconds, interrupting active tasks.
forward() {
	stopping=1
	if [ "$child" -ne 0 ]; then
		kill -TERM "$child" 2>/dev/null || true
	fi
}
trap forward INT TERM

delay=1
while :; do
	"$BIN" "$@" &
	child=$!

	# A signal interrupts wait with an exit status greater than 128 while the child
	# is still shutting down gracefully. Wait again to obtain its actual exit code.
	wait "$child"
	code=$?
	if [ "$code" -gt 128 ]; then
		wait "$child"
		code=$?
	fi
	child=0

	if [ "$stopping" -eq 1 ]; then
		echo "[artex] Stopped"
		exit 0
	fi

	case "$code" in
		0)
			echo "[artex] Exited normally"
			exit 0
			;;
		"$RESTART_CODE")
			# Update/rollback is ready; artex applies it on startup (see selfupdate.Bootstrap).
			echo "[artex] Restart requested (applying the new version)..."
			delay=1
			;;
		*)
			echo "[artex] Unexpected exit (code=$code); restarting in ${delay}s" >&2
			sleep "$delay"
			delay=$((delay * 2))
			[ "$delay" -gt "$MAX_DELAY" ] && delay=$MAX_DELAY
			;;
	esac
done
