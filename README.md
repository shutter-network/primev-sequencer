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
