#!/usr/bin/env bash
set -e

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -W)"
exec powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$script_dir/local-db.ps1" "${1:-status}"
