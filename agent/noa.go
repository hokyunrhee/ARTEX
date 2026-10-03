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

// noa is the "model-driven context compaction" mechanism introduced in norma
// v0.4.0, exposed as a platform experimental feature that users toggle in system
// settings. It is mutually exclusive with the built-in compaction: noaadapter.Enable
// is the only entry point, wiring up the context takeover hook (Compactor), the
// Compress tool and three resident prompt segments in one call; not calling Enable
// means it is off (built-in compaction keeps working). The toggle is resolved by the
// noaEnabledFn each agent injects, read once per run, so flipping it affects only
// runs started afterward and needs no agent rebuild.

// enableNoa wires noa into opts when the resolver reports it is on. archiveRoot is the
// persistence base directory for the pre-compaction originals (it takes the global
// workDir, so every agent lands under <workDir>/noa rather than scattering across
// task/intent directories); sessionID names the archive subdirectory beneath it
// (globally unique, so there is no collision within the same base directory).
//
// noa is an experimental feature: a wiring failure must never interrupt a real task.
// On error it is reported via onWarn and falls back to the built-in compaction. On
// successful enablement it clears opts.Compaction to avoid agentcore warning that
// "two context managers are set at once".
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
			onWarn("noa compaction failed to enable, falling back to built-in compaction: " + err.Error())
		}
		return
	}
	// Compactor overrides Compaction, but agentcore warns every time both are set; clear it explicitly.
	opts.Compaction = nil
}
