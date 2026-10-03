#!/usr/bin/env bash
# Validate source text against exact, occurrence-counted legacy exceptions.
# Scope: tracked and untracked nonignored files, plus all docs/ files. Generated
# release/build trees stay excluded through Git ignores and are checked separately.
# Each exception is: path<TAB>SHA256 of the entire line<TAB>request section<TAB>reason.
# Identical permitted lines require one entry per occurrence. Changes, duplicates,
# missing lines, and malformed entries fail rather than silently extending permission.
# The character ranges match the migration gate; audit smart punctuation separately.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
python3 - <<'PY'
from collections import Counter
from pathlib import Path
import hashlib
import re
import subprocess
import sys

root = Path.cwd()
allowlist = root / 'scripts/i18n-allowlist.txt'
pattern = re.compile('[\u4e00-\u9fff\u3400-\u4dbf\u3000-\u303f\uff00-\uffef\uac00-\ud7af\u3040-\u30ff]')
# Decode bytes directly rather than relying on binary-file detection. This is
# equivalent to rg -a for valid UTF-8 source and preserves embedded NUL bytes.
paths = set(filter(None, subprocess.check_output(['git', 'ls-files', '-c', '-o', '--exclude-standard', '-z']).decode().split('\0')))
if (root / 'docs').exists():
    paths.update(str(p.relative_to(root)) for p in (root / 'docs').rglob('*') if p.is_file())
excluded = {'.png', '.ico', '.jpg', '.jpeg', '.gif', '.webp', '.woff', '.woff2', '.ttf', '.zip', '.pdf'}
expected = Counter()
errors = []
if not allowlist.is_file():
    errors.append(f'Missing allowlist: {allowlist}')
else:
    for number, line in enumerate(allowlist.read_text(encoding='utf-8').splitlines(), 1):
        if not line.strip() or line.startswith('#'):
            continue
        fields = line.split('\t')
        if (len(fields) != 4 or not re.fullmatch('[a-f0-9]{64}', fields[1])
                or not re.fullmatch(r'(?:4[AB]\.[1-9][0-9]*|5)', fields[2])
                or not fields[3].strip() or not fields[3].isascii()):
            errors.append(f'{allowlist}:{number}: malformed exception')
            continue
        path, digest, section, reason = fields
        if Path(path).is_absolute() or '..' in Path(path).parts or path not in paths:
            errors.append(f'{allowlist}:{number}: exception path is outside source scope')
            continue
        expected[(path, digest)] += 1
remaining = expected.copy()
offending_files = set()
for path in sorted(paths):
    if pattern.search(path):
        offending_files.add(path)
        print(f'{path}: CJK filename must be renamed', file=sys.stderr)
    target = root / path
    if target.is_symlink():
        errors.append(f'{path}: source symlink must be classified explicitly')
        continue
    if not target.is_file() or target.suffix.lower() in excluded:
        continue
    try:
        data = target.read_bytes().decode('utf-8')
    except UnicodeDecodeError:
        errors.append(f'{path}: unsupported non-UTF-8 source; classify this file explicitly')
        continue
    for number, raw_line in enumerate(data.split('\n'), 1):
        line = raw_line.removesuffix('\r')
        if not pattern.search(line):
            continue
        key = (path, hashlib.sha256(line.encode('utf-8')).hexdigest())
        if remaining[key] > 0:
            remaining[key] -= 1
        else:
            offending_files.add(path)
            print(f'{path}:{number}: {line}', file=sys.stderr)
for (path, digest), count in remaining.items():
    if count:
        errors.append(f'{path}: {count} stale exception(s) for {digest}')
for error in errors:
    print(error, file=sys.stderr)
print(len(offending_files))
sys.exit(bool(offending_files or errors))
PY
