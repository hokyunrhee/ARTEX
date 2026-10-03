#!/bin/sh
# ARTEX supervisor launch script (Linux / macOS / Docker ENTRYPOINT)
#
# Usage:
#   ./start.sh                       Run in the foreground (Ctrl-C to stop)
#   nohup ./start.sh >artex.log 2>&1 &   Run as a background daemon
#   ./start.sh -addr :9000           Extra arguments are passed through to artex as-is
#
# It does one thing only: start artex, and after the process exits, decide from the exit
# code whether to relaunch it.
#
#   0      Normal user stop          -> exit the loop
#   75     Program requested restart -> relaunch immediately (the user clicked "one-click
#          update" or "roll back" in the UI)
#   other  Crash                     -> back off, then relaunch (1->2->4...up to 60 seconds)
#
# Downloading, SHA256 verification, and binary swapping are deliberately not done here: that
# logic would need two copies, one for sh and one for bat, and it is precisely the part that
# must never go wrong -- once a non-working binary is swapped in, this script would
# faithfully relaunch it over and over, leaving the user no choice but to fix it by hand on
# the machine. So all verification/swapping stays in Go (the selfupdate package) and is done
# by artex itself at startup, keeping the script dead simple.
set -u

cd "$(dirname "$0")" || exit 1

BIN=./artex
[ -x "$BIN" ] || { echo "[artex] executable not found: $BIN" >&2; exit 1; }

RESTART_CODE=75
MAX_DELAY=60

child=0
stopping=0

# Forward the stop signal to artex itself.
#
# This is required under Docker: docker stop sends SIGTERM only to PID 1 (that is, this
# script), not to the child process. Without forwarding, artex never receives the signal,
# cannot shut down gracefully, and is hard-killed by SIGKILL after 10 seconds, cutting any
# running task off midway.
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

	# A signal interrupts wait and makes it return >128. At that point the child is still
	# shutting down gracefully, so we must wait once more to get its real exit code.
	wait "$child"
	code=$?
	if [ "$code" -gt 128 ]; then
		wait "$child"
		code=$?
	fi
	child=0

	if [ "$stopping" -eq 1 ]; then
		echo "[artex] stopped"
		exit 0
	fi

	case "$code" in
		0)
			echo "[artex] exited normally"
			exit 0
			;;
		"$RESTART_CODE")
			# Update/rollback is ready: after relaunch, artex completes the swap at startup (see selfupdate.Bootstrap).
			echo "[artex] restart requested (applying the new version)…"
			delay=1
			;;
		*)
			echo "[artex] abnormal exit (code=$code), restarting in ${delay}s" >&2
			sleep "$delay"
			delay=$((delay * 2))
			[ "$delay" -gt "$MAX_DELAY" ] && delay=$MAX_DELAY
			;;
	esac
done
