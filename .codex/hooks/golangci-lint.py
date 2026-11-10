#!/usr/bin/env python3
"""Run golangci-lint after Codex edits a Go source file."""

import json
import os
from pathlib import Path
import re
import subprocess
import sys


PATCH_FILE = re.compile(r"(?m)^\*\*\* (?:Add|Update|Delete) File:\s*(.+?)\s*$")
DIFF_FILE = re.compile(r"(?m)^\+\+\+ (?:[ab]/)?([^\t\n]+)")
PATH_KEYS = {"file_path", "filepath", "path", "filename"}


def input_paths(value):
    if isinstance(value, dict):
        for key, nested in value.items():
            if key.lower() in PATH_KEYS and isinstance(nested, str):
                yield nested
            yield from input_paths(nested)
    elif isinstance(value, list):
        for nested in value:
            yield from input_paths(nested)
    elif isinstance(value, str):
        yield from PATCH_FILE.findall(value)
        yield from DIFF_FILE.findall(value)


def is_project_go_file(path_text, root, cwd):
    path_text = path_text.strip().strip("\"'")
    if not path_text or path_text == "/dev/null":
        return False

    path = Path(path_text)
    if not path.is_absolute():
        path = cwd / path
    try:
        path = path.resolve()
        path.relative_to(root)
    except (OSError, ValueError):
        return False
    return path.suffix == ".go"


def main():
    try:
        event = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        print(f"golangci-lint hook: invalid Codex hook input: {exc}", file=sys.stderr)
        return 2

    cwd = Path(event.get("cwd") or os.getcwd()).resolve()
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--show-toplevel"],
            cwd=cwd,
            capture_output=True,
            text=True,
            check=True,
        )
    except (OSError, subprocess.CalledProcessError) as exc:
        print(f"golangci-lint hook: cannot find the project Git root: {exc}", file=sys.stderr)
        return 2

    root = Path(result.stdout.strip()).resolve()
    if not any(is_project_go_file(path, root, cwd) for path in input_paths(event.get("tool_input", {}))):
        return 0

    try:
        lint = subprocess.run(
            ["golangci-lint", "run"],
            cwd=root,
            capture_output=True,
            text=True,
        )
    except OSError as exc:
        print(f"golangci-lint hook: could not run golangci-lint: {exc}", file=sys.stderr)
        return 2

    if lint.returncode == 0:
        return 0

    print("golangci-lint failed after a Go source edit:", file=sys.stderr)
    if lint.stdout:
        print(lint.stdout, file=sys.stderr, end="")
    if lint.stderr:
        print(lint.stderr, file=sys.stderr, end="")
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
