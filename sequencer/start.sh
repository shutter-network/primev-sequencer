#!/bin/bash

# PrimeV PoC Sequencer Start Script
# This script builds and runs the sequencer with the required Shutter Network contract addresses

set -e

# Source .env file if it exists
if [ -f .env ]; then
    echo "Loading environment variables from .env file..."
    set -a  # automatically export all variables
    source .env
    set +a  # turn off automatic export
else
    echo "No .env file found. Using environment variables or defaults."
    echo "   To use a .env file, copy config.env.example to .env and update the values."
fi

echo "Building PrimeV sequencer..."
go build -o primev-sequencer .

if [ $? -ne 0 ]; then
    echo "Build failed!"
    exit 1
fi

echo "Build successful!"

# Default configuration
RPC_PORT="${RPC_PORT:-8545}"
UPSTREAM_RPC="${UPSTREAM_RPC:-http://localhost:8546}"
LOG_LEVEL="${LOG_LEVEL:-info}"

# Required contract addresses - these must be set as environment variables
# or provided as arguments to this script
KEYPER_SET_MANAGER_ADDRESS="${KEYPER_SET_MANAGER_ADDRESS:-}"
KEY_BROADCAST_ADDRESS="${KEY_BROADCAST_ADDRESS:-}"

# Check if contract addresses are provided
if [ -z "$KEYPER_SET_MANAGER_ADDRESS" ]; then
    echo "KEYPER_SET_MANAGER_ADDRESS environment variable is required"
    echo "   Example: export KEYPER_SET_MANAGER_ADDRESS=0x1234567890123456789012345678901234567890"
    echo "   Or create a .env file based on config.env.example"
    exit 1
fi

if [ -z "$KEY_BROADCAST_ADDRESS" ]; then
    echo "KEY_BROADCAST_ADDRESS environment variable is required"
    echo "   Example: export KEY_BROADCAST_ADDRESS=0x1234567890123456789012345678901234567890"
    echo "   Or create a .env file based on config.env.example"
    exit 1
fi

echo "Starting PrimeV sequencer..."
echo "   RPC Port: $RPC_PORT"
echo "   Upstream RPC: $UPSTREAM_RPC"
echo "   Log Level: $LOG_LEVEL"
echo "   KeyperSetManager: $KEYPER_SET_MANAGER_ADDRESS"
echo "   KeyBroadcast: $KEY_BROADCAST_ADDRESS"
echo ""

# Start the sequencer
./primev-sequencer \
    --rpc-port "$RPC_PORT" \
    --upstream-rpc "$UPSTREAM_RPC" \
    --log-level "$LOG_LEVEL" \
    --keyper-set-manager-address "$KEYPER_SET_MANAGER_ADDRESS" \
    --key-broadcast-address "$KEY_BROADCAST_ADDRESS" \
    --grpc-addr "$GRPC_ADDR"