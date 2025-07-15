#!/bin/bash

echo "🧪 Building and running PrimeV RPC Test Client..."
echo ""

# Build the test client
echo "🔨 Building test client..."
go build -o test_client ./main.go

if [ $? -ne 0 ]; then
    echo "❌ Build failed!"
    exit 1
fi

echo "✅ Build successful!"
echo ""

# Run the test client
echo "🚀 Running test client..."
echo "📡 Make sure your PrimeV sequencer is running on localhost:8546"
echo ""

# Set the hardcoded private key for testing
PRIVATE_KEY=0xd8e7f105e4b0a1a3243f748f7e7e3e0d682c8e8ed2b76c69be5adfc34b3fa58f

echo "🔑 Using hardcoded test private key"
echo "🔐 Private Key: ${PRIVATE_KEY:0:10}...${PRIVATE_KEY: -8}"
echo ""

# Export the private key and run the test client
export PRIVATE_KEY
./test_client 