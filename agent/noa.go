package agent

import (
	"log"
	"path/filepath"

	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/noaadapter"
)

// noaWarn returns a diagnostics sink tagging non-fatal noa messages with the
// session, routed through the package logger (agents have no per-instance one).
func noaWarn(session string) func(string) {
	return func(msg string) { log.Printf("[noa] %s: %s", session, msg) }
}

// noa is model-driven context compaction introduced in norma v0.4.0, exposed as an experimental platform feature
// toggled in system settings. It is mutually exclusive with built-in compaction: noaadapter.Enable is the only entry point,
// attaching the context manager (Compactor), Compress tool, and three permanent prompt blocks together. Without Enable,
// noa is disabled and built-in compaction works normally. Each agent's noaEnabledFn resolves the switch once per run,
// so changes affect subsequent runs without rebuilding agents.

// enableNoa integrates noa into opts when the resolver reports it enabled. archiveRoot is the persistent base directory
// for archived original content (the global workDir; all agents use <workDir>/noa, not scattered task/intent directories).
// sessionID names its archive subdirectory and is globally unique, avoiding collisions within the shared base.
//
// noa is experimental: integration failures must not interrupt real tasks. Report errors via onWarn and fall back to built-in compaction.
// On success, clear opts.Compaction so agentcore does not warn about two context managers being set.
func enableNoa(opts *agentcore.Options, enabled func() bool, archiveRoot, sessionID string, onWarn func(string)) {
	if enabled == nil || !enabled() {
		return
	}
	if opts.OnWarn == nil {
		opts.OnWarn = onWarn
	}
	if err := noaadapter.Enable(opts, noaadapter.Options{
		ArchiveBaseDir: filepath.Join(archiveRoot, "noa"),
		SessionID:      sessionID,
		OnWarn:         onWarn,
	}); err != nil {
		if onWarn != nil {
			onWarn("failed to enable noa compaction; falling back to built-in compaction: " + err.Error())
		}
		return
	}
	// Compactor overrides Compaction, but agentcore warns on every run when both exist; clear it explicitly.
	opts.Compaction = nil
}
