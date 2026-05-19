package twilioadapter

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
)

// GenerateApprovalCode returns a 6-digit numeric code drawn from a
// crypto-strong RNG. ADR-0005 T6.
func GenerateApprovalCode() (string, error) {
	max := big.NewInt(1_000_000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// HashApprovalCode returns hex-encoded SHA-256 of the raw code. The
// adapter persists the hash, never the raw code (ADR-0005 T32).
func HashApprovalCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// MaskPhone returns a masked phone of the form `+1***1234` per
// ADR-0005 T35: country code visible + last 4 visible, middle replaced
// with ***. Input is assumed to be E.164 (`+1...`); if malformed,
// returns the input unchanged.
func MaskPhone(phone string) string {
	if len(phone) < 6 || phone[0] != '+' {
		return phone
	}
	// Find country code: digits immediately after '+' up to a length-aware
	// split. For overnight use we assume 1- or 2-digit country codes; the
	// last 4 digits are always preserved.
	cc := 2 // default: "+1" style
	if len(phone) > 12 {
		cc = 3 // "+44" or "+91"
	}
	if len(phone) < cc+4 {
		return phone
	}
	last4 := phone[len(phone)-4:]
	return phone[:cc] + "***" + last4
}
