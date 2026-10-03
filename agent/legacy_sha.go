package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// PromptSHA256 is the digest of a prompt body, matching how LegacyPromptSHA256 was
// generated (sha256 of the raw template text).
func PromptSHA256(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// IsLegacyPromptBody reports whether body is one of the historical Chinese default
// bodies for key — i.e. an unmodified default that the English migration may upgrade.
func IsLegacyPromptBody(key, body string) bool {
	set, ok := LegacyPromptSHA256[key]
	if !ok {
		return false
	}
	_, hit := set[PromptSHA256(body)]
	return hit
}

// ToolDescSHA256 digests a tool description (raw bytes), matching LegacyToolDescSHA256.
func ToolDescSHA256(desc string) string { return PromptSHA256(desc) }

// ToolSchemaSHA256 digests a tool schema the same way the fingerprints were generated:
// unmarshal the stored JSON and re-marshal it so a jsonb column round-trips to the same
// canonical bytes (Go sorts object keys, HTML-escapes), then sha256. Returns "" on bad JSON.
func ToolSchemaSHA256(schema json.RawMessage) string {
	var v any
	if err := json.Unmarshal(schema, &v); err != nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// IsLegacyToolDesc / IsLegacyToolSchema report whether a tool row still holds one of the
// historical Chinese defaults for key.
func IsLegacyToolDesc(key, desc string) bool {
	set, ok := LegacyToolDescSHA256[key]
	if !ok {
		return false
	}
	_, hit := set[ToolDescSHA256(desc)]
	return hit
}

func IsLegacyToolSchema(key string, schema json.RawMessage) bool {
	set, ok := LegacyToolSchemaSHA256[key]
	if !ok {
		return false
	}
	_, hit := set[ToolSchemaSHA256(schema)]
	return hit
}
