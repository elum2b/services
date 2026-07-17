package sql

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// CreateKey builds a stable cache key from arbitrary values.
func CreateKey(parts ...any) string {
	raw := fmt.Sprintf("%#v", parts)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
