# PrimeV PoC - Sequencer

A proof-of-concept implementation of a sequencer for the PrimeV protocol, integrating Shutter Network's threshold cryptography for encrypted transaction ordering and MEV protection.

## 🏗️ Architecture Overview

The PrimeV sequencer is a multi-module system that handles encrypted transactions, participates in MEV-commit bidding, and manages the complete lifecycle from transaction submission to blockchain finalization. The system combines privacy-preserving threshold cryptography with automated bidding to protect against MEV attacks.

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   RPC Server    │    │   REST API      │    │ P2P Messaging   │
│   (Port 8545)   │    │   (Port 8080)   │    │ (Libp2p/Shutter)│
└─────────┬───────┘    └─────────┬───────┘    └─────────┬───────┘
          │                      │                      │
          └──────────────────────┼──────────────────────┘
                                 │
                    ┌────────────▼────────────┐
                    │   Transaction Handler   │
                    │   (Status Management)   │
                    └────────────┬────────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              │                  │                  │
    ┌─────────▼──────┐  ┌────────▼────────┐  ┌─────▼──────┐
    │ Bid Manager    │  │ Shutter Engine  │  │ TX Verifier│
    │ (MEV-Commit)   │  │ (Encryption)    │  │ (Blockchain)│
    └────────────────┘  └─────────────────┘  └────────────┘
```

## 📦 Package Breakdown

### 🚀 Main Application (`main.go`)

**Purpose**: Application entry point and orchestration layer

**Key Responsibilities**:
- Environment configuration and validation
- Module initialization and lifecycle management
- Graceful shutdown handling
- P2P network setup with Shutter Network integration

**Key Components**:
- CLI interface using Cobra
- Configuration management via environment variables
- Service orchestration with integrated health monitoring
- P2P bootstrap configuration for Shutter Network

### 💾 Transaction Handler (`txhandler/`)

**Purpose**: Core transaction state management and storage

**Transaction Lifecycle**:
```
init → bidsubmitted → committed → decrypted → finalised
                                            ↓
                              blocked ←─────┘
```

**Key Features**:
- Thread-safe in-memory transaction storage
- Status-based transaction querying
- Identity-based transaction lookup for keyper integration
- Comprehensive transaction metadata tracking

**Identity Management**:
- **Unique Identity Creation**: Each transaction gets a unique identity created by hashing 32 random bytes with the bidder node address
- **Cryptographic Generation**: `Identity = Keccak256(Random32Bytes + BidderNodeAddress)`
- **Random Prefix**: Uses cryptographically secure random bytes to ensure unpredictability
- **Keyper Verification**: Keypers can verify identities by extracting the bidder node address from bid signatures in commitments
- **No Re-registration**: Unique identities prevent duplicate processing and ensure cryptographic integrity

**Transaction States**:
- `init`: Newly submitted, awaiting bid inclusion
- `bidsubmitted`: Included in MEV-commit bid
- `committed`: Commitment received from providers
- `decrypted`: Decryption keys received via P2P
- `finalised`: Verified on blockchain
- `blocked`: Failed verification after retries

### 🔐 Shutter Engine (`shutter/`)

**Purpose**: Threshold cryptography integration with Shutter Network

**Core Functionality**:
- **Threshold Encryption**: Encrypts transactions using Shutter's time-lock encryption
- **Eon Management**: Handles cryptographic epochs for time-based decryption
- **Identity Generation**: Creates unique identities for encrypted transactions
- **Blockchain Integration**: Fetches eon keys and manages keyper sets

**Encryption Process**:
1. Calculate target block for transaction inclusion
2. Determine cryptographic eon for target block
3. Fetch eon public key from KeyBroadcast contract
4. Generate unique identity for transaction
5. Encrypt transaction data using threshold cryptography

**Identity Generation & Verification**:
- **Cryptographic Identity**: `Identity = Keccak256(Random32Bytes || BidderNodeAddress)`
- **Random Identity Prefix**: Each transaction uses 32 cryptographically secure random bytes as identity prefix
- **Bidder Binding**: Identity is cryptographically bound to the bidder who submitted the transaction
- **Keyper Verification**: Keypers verify identities by:
  1. Extracting bidder address from bid signature in commitment message
  2. Recomputing identity using the random prefix from encryption and extracted bidder address
  3. Comparing computed identity with the one used for encryption
- **Unpredictable Identities**: Random prefixes make identities unpredictable and prevent pre-computation attacks

### 💰 Bid Manager (`bidder/`)

**Purpose**: MEV-commit integration and automated bidding

**Bidding Strategy**:
- Aggregates `init` status transactions into bids
- Creates time-decay bids with configurable parameters
- Submits bids to MEV-commit providers via gRPC
- Handles commitment processing and validation

**Bid Parameters**:
- Fixed bid amount (30 Gwei)
- Configurable slash conditions
- Time-decay mechanism for bid value
- Block-based inclusion windows

### 🌐 P2P Messaging (`p2p/`)

**Purpose**: Shutter Network communication and key distribution

**Message Types**:
- **DecryptionKeys**: Received from keypers when time-lock expires
- **Commitments**: Sent to keypers after successful bid commitments

**Key Features**:
- Libp2p-based networking with Shutter Network
- Message validation and filtering
- Automatic transaction status updates on key receipt
- Provider address verification

### 🔍 Transaction Verifier (`transaction_verifier/`)

**Purpose**: Blockchain verification and finalization

**Verification Process**:
1. Periodically scans for `decrypted` transactions
2. Queries blockchain for transaction inclusion
3. Updates status to `finalised` if found
4. Implements retry logic with automatic fallback to `blocked`

**Key Features**:
- Configurable verification intervals
- Ethereum client integration
- Comprehensive error handling
- Retry mechanism with maximum attempt limits

### 🌐 RPC Server (`rpc/`)

**Purpose**: Ethereum JSON-RPC compatibility with encryption

**Supported Methods**:
- `eth_sendRawTransaction`: Intercepts and encrypts transactions
- Standard Ethereum RPC proxy for other methods

**Transaction Flow**:
1. Receive raw transaction via JSON-RPC
2. Encrypt using Shutter threshold cryptography
3. Store in transaction handler with `init` status
4. Return transaction hash to client
5. Proxy other RPC calls to upstream Ethereum node

### 🔗 REST API (`api/`)

**Purpose**: HTTP API for transaction introspection

**Endpoints**:
- `GET /get_decrypted_tx/{txHash}`: Retrieve and decrypt transaction data

**Features**:
- Transaction status validation
- Real-time decryption of committed transactions
- Comprehensive error handling and logging
- Will be used by Builders to get the decrypted transaction

### 🛠️ Utilities (`utils/`)

**Purpose**: Shared cryptographic and helper functions

**Key Functions**:
- Identity prefix generation for Shutter integration
- Cryptographic utilities for transaction hashing
- Common data structures and constants

## 🔄 Complete Transaction Flow

### 1. Transaction Submission
```
Client → RPC Server → Shutter Engine → Transaction Handler
  │         │              │                    │
  │         │              └── Encrypt          │
  │         └── JSON-RPC         │              │
  │                              └── Store ─────┘
  └── Return TxHash
```

### 2. Bidding Process
```
Transaction Handler → Bid Manager → MEV-Commit → P2P Network
       │                 │           Provider        │
       │                 └── Create Bid     │        │
       └── Status: init           │         │        │
                                  └── gRPC  │        │
                                           │         │
                                    Commitment ──────┘
```

### 3. Key Distribution & Decryption
```
Shutter Keypers → P2P Network → Message Handler → Transaction Handler
       │              │              │                    │
       │              │              └── Update Status    │
       └── Broadcast  │                       │           │
           Keys       └── DecryptionKeys      │           │
                                              └── Status: decrypted
```

### 4. Verification & Finalization
```
Transaction Verifier → Ethereum Node → Transaction Handler
         │                   │               │
         │                   └── Query       │
         └── Check Status          │         │
                                   └── Update Status
                                        (finalised/blocked)
```

## ⚙️ Configuration

### Contract Addresses

The sequencer integrates with Shutter Network smart contracts:

- **KeyperSetManager**: Manages cryptographic epochs and keyper sets
- **KeyBroadcast**: Distributes eon public keys for encryption
- **MEV-Commit**: Provider network for bid submission and commitments

## 🚀 Quick Start

### 1. Environment Setup
```bash
# Copy configuration template
cp sequencer/config.env.example sequencer/.env

# Update configuration with your values
nano sequencer/.env
```

### 2. Build & Run
```bash
cd sequencer
chmod +x start.sh
./start.sh
```

### 3. Submit Test Transaction
```bash
# Submit encrypted transaction
curl -X POST http://localhost:8545 \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "eth_sendRawTransaction",
    "params": ["0x..."],
    "id": 1
  }'
```

### 4. Monitor Status
```bash
# Check transaction status via REST API
curl http://localhost:8080/get_decrypted_tx/0x...
```

### Testing
A separate test client is provided in `test_client/` for transaction sending:

```bash
cd test_client
./run_test.sh
```

## 🐳 Docker Setup

### Using Docker Compose (Recommended)

1. **Environment Configuration**
   ```bash
   # Copy the Docker environment template
   cp docker-compose.env.example .env

   # Edit configuration with your values
   nano .env
   ```

2. **Build and Run**
   ```bash
   # Build and start the sequencer
   docker compose up --build

   # Or run in background
   docker compose up -d --build
   ```

3. **View Logs**
   ```bash
   # View logs
   docker compose logs -f sequencer

   # Stop the service
   docker compose down
   ```

### Manual Docker Commands

If you prefer to use Docker directly:

```bash
# Build the image
docker build -t primev-sequencer .

# Run the sequencer container with configurable environment variables
docker run -p 8545:8545 -p 8080:8080 -p 23003:23003 \
  --network primev-poc_default \
  -e UPSTREAM_RPC_URL="https://0xrpc.io/hoodi" \
  -e LOG_LEVEL="info" \
  -e VERIFICATION_INTERVAL="10s" \
  -e BIDDER_NODE_ADDRESS="0x1234567890123456789012345678901234567890" \
  -e P2P_KEY="your_p2p_private_key_here" \
  primev-sequencer

# Note: For a complete setup, you'll also need to run the bidder container manually
# and ensure proper networking between containers. Docker Compose is recommended.
```

### Configurable Environment Variables

For production deployment, you can configure these environment variables (defaults shown in parentheses):

**Sequencer Service:**
- `UPSTREAM_RPC_URL`: Ethereum RPC endpoint for transaction verification (https://0xrpc.io/hoodi)
- `LOG_LEVEL`: Logging level (info)
- `VERIFICATION_INTERVAL`: How often to check transaction status on blockchain (10s)
- `BIDDER_NODE_ADDRESS`: Your bidder node address (0x1234567890123456789012345678901234567890)
- `P2P_KEY`: Your libp2p private key for P2P networking (your_p2p_private_key_here)

**Bidder Service:**
- `DOMAIN`: MEV-commit domain (testnet.mev-commit.xyz)
- `SETTLEMENT_RPC_URL`: Settlement layer RPC URL (https://chainrpc.testnet.mev-commit.xyz/)
- `LOG_LEVEL`: Logging level (info)
- `LOG_FMT`: Log format (text)
- `AUTO_DEPOSIT_AMOUNT`: Automatic deposit amount in wei (1000000000000000000)

### Ports

- **8545**: Ethereum JSON-RPC API (encrypted transaction submission)
- **8080**: REST API (transaction status and decryption)
- **23003**: P2P networking (Shutter Network communication)

### Health Checks

The Docker container includes health checks that verify the REST API is responding. You can monitor the health status with:

```bash
docker ps
# Look for "healthy" status in the STATUS column
```
