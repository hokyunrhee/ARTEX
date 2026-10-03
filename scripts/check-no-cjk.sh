#!/usr/bin/env bash
# check-no-cjk.sh — fail if any tracked source file contains CJK or full-width
# punctuation outside the committed allowlist. Part of the English-only gate: run it
# after every change so future development stays mechanical.
#
# The allowlist (scripts/i18n-allowlist.txt) holds the deliberate exceptions as
# "path:matched-line-content" entries (blank lines and lines starting with # are
# ignored). Matching on content rather than line number keeps it stable as files are
# edited. The exceptions are the real-world-data markers, the frozen Chinese
# fingerprints the seed migrations compare against, and the Unicode-handling test
# vectors. See docs/glossary.md and the conversion report for the rationale.
set -euo pipefail
cd "$(dirname "$0")/.."

ALLOW="scripts/i18n-allowlist.txt"
# CJK ideographs + CJK symbols/punctuation + full-width forms + Hangul + Kana.
PAT='[\x{4E00}-\x{9FFF}\x{3400}-\x{4DBF}\x{3000}-\x{303F}\x{FF00}-\x{FFEF}\x{AC00}-\x{D7AF}\x{3040}-\x{30FF}]'

# All tracked files except binaries. -a keeps NUL-containing files in scope
# (web/src/components/exploration-graph.tsx holds two deliberate NUL bytes).
mapfile -t files < <(git ls-files \
  ':!:*.png' ':!:*.ico' ':!:*.jpg' ':!:*.jpeg' ':!:*.gif' ':!:*.webp' \
  ':!:*.woff' ':!:*.woff2' ':!:*.ttf' ':!:scripts/i18n-allowlist.txt' \
  ':!:docs/glossary.md')  # the glossary is the Chinese->English mapping; bilingual by design

# grep -o emits "path:matched-text" (the matched line's CJK-bearing portion). We match
# whole lines, so -o would split; instead print "path:line-content" and compare.
raw="$(LC_ALL=C.UTF-8 grep -naP "$PAT" "${files[@]}" 2>/dev/null || true)"
# raw lines look like "path:lineno:content"; reduce to "path:content" for allowlisting.
reduced="$(printf '%s\n' "$raw" | sed -E 's/^([^:]+):[0-9]+:/\1:/')"

allow_tmp="$(mktemp)"
trap 'rm -f "$allow_tmp"' EXIT
grep -vE '^\s*(#|$)' "$ALLOW" 2>/dev/null > "$allow_tmp" || true

hits="$(printf '%s\n' "$reduced" | grep -vxF -f "$allow_tmp" 2>/dev/null | grep -v '^$' || true)"

if [[ -n "$hits" ]]; then
  printf '%s\n' "$hits"
  echo "---"
  echo "FAIL: $(printf '%s\n' "$hits" | grep -c .) CJK/full-width occurrence(s) outside the allowlist."
  exit 1
fi
echo "0"
