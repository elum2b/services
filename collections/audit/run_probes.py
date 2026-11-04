#!/usr/bin/env python3
"""Run the permanent audit regression tests against the fixed implementation."""
import pathlib
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parents[2]
args = sys.argv[1:] or ["-race", "-run", "^TestRegression", "-count=1", "-v"]
raise SystemExit(subprocess.call(["go", "test", "./collections", *args], cwd=root))
