#!/usr/bin/env python3
"""Enforce per-package total and handwritten Go statement coverage."""
import argparse
import math
from pathlib import Path

MODULE = 'github.com/infoe-labs/pikvm-cli/'
PACKAGES = ('cli', 'lib/pikvm', 'mcpserver')

def measure(text):
    lines = text.splitlines()
    if not lines or lines[0] not in ('mode: atomic', 'mode: count', 'mode: set'):
        raise ValueError('invalid coverage profile header')
    blocks = {}
    for line in lines[1:]:
        if not line.strip(): continue
        try:
            location, size, hits = line.split()
            filename, span = location.rsplit(':', 1)
            size, hits = int(size), int(hits)
            if size < 0 or hits < 0 or not span: raise ValueError()
        except ValueError as exc:
            raise ValueError('invalid coverage block') from exc
        key = filename, span
        previous = blocks.get(key)
        if previous and previous[0] != size: raise ValueError('inconsistent coverage block size')
        blocks[key] = size, hits > 0 or bool(previous and previous[1])
    result = {}
    for package in PACKAGES:
        prefix = MODULE + package + '/'
        for kind in ('all', 'handwritten'):
            total = covered = 0
            for (filename, _), (size, hit) in blocks.items():
                if not filename.startswith(prefix): continue
                if kind == 'handwritten' and filename.endswith(('/services.go', '/endpoints.go')): continue
                total += size
                if hit: covered += size
            if not total: raise ValueError('missing coverage for ' + package + ' (' + kind + ')')
            result[package, kind] = covered / total * 100
    return result

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('profile', type=Path)
    parser.add_argument('--minimum', type=float, default=90)
    parser.add_argument('--summary', type=Path)
    args = parser.parse_args()
    if not math.isfinite(args.minimum) or not 0 <= args.minimum <= 100: parser.error('minimum must be between 0 and 100')
    try: result = measure(args.profile.read_text())
    except (OSError, ValueError) as exc: parser.exit(1, str(exc) + '\n' if isinstance(exc,ValueError) else 'cannot read coverage profile\n')
    rows = ['| Package | All statements | Handwritten |', '| --- | --- | --- |']
    for package in PACKAGES: rows.append(f'| `{package}` | {result[package,"all"]:.2f}% | {result[package,"handwritten"]:.2f}% |')
    report = '\n'.join(rows) + '\n'
    print(report, end='')
    if args.summary:
        with args.summary.open('a') as stream: stream.write(report)
    if any(value < args.minimum for value in result.values()): parser.exit(1, 'coverage is below the required floor\n')

if __name__ == '__main__': main()
