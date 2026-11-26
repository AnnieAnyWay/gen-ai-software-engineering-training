#!/usr/bin/env bash
# Starts the Banking Transactions API (default port 3000, override with PORT=8080).
set -euo pipefail
cd "$(dirname "$0")/../src"
go run ./cmd/server
