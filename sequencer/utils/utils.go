package utils

import (
	"bytes"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func GetIdentity(identityPrefix []byte, bidderNodeAddress string) []byte {
	bidderNodeAddressHex := common.HexToAddress(bidderNodeAddress)
	var buf bytes.Buffer
	buf.Write(identityPrefix)
	buf.Write(bidderNodeAddressHex.Bytes())
	return crypto.Keccak256(buf.Bytes())
}
