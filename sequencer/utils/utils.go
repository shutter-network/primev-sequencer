package utils

import (
	"bytes"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func GetIdentityPrefix(txHash common.Hash, bidderNodeAddress string) []byte {
	bidderNodeAddressHex := common.HexToAddress(bidderNodeAddress)
	var buf bytes.Buffer
	buf.Write(txHash.Bytes())
	buf.Write(bidderNodeAddressHex.Bytes())
	return crypto.Keccak256(buf.Bytes())
}
