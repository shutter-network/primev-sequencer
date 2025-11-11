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

echo "Starting PrimeV sequencer..."

# Start the sequencer (only log-level is passed as CLI flag)
./primev-sequencer --log-level "${LOG_LEVEL:-info}"