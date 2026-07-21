#!/usr/bin/env python3
"""stockctl weekend seed launcher, version 1.

This intentionally contains no seed, retry, cache, or scheduling policy. Those
belong to stockctl; this wrapper only replaces itself with the requested CLI.
"""
from __future__ import annotations

import os
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
binary = os.environ.get("STOCKCTL_BIN")
if binary:
    command = [binary]
else:
    candidate = ROOT / "bin" / "stockctl"
    command = [str(candidate)] if candidate.is_file() else ["go", "run", "."]

os.execvp(command[0], command + ["seed", "history", *sys.argv[1:]])
