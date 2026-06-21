#!/bin/bash
set -euo pipefail

# Usage: ./launch_node.sh <node_number> [reset_flag]
# Example: ./launch_node.sh 1 true

i="${1:-}"
RESET_FLAG="${2:-true}"

if [[ -z "$i" ]]; then
  echo "Usage: $0 <node_number> [reset_flag]"
  exit 1
fi

PEERS="node1=127.0.0.1:9001,node2=127.0.0.1:9002,node3=127.0.0.1:9003"
ID="node$i"
PORT="800$i"

go run main.go --id="$ID" --port="$PORT" --peers="$PEERS" --reset="$RESET_FLAG"
