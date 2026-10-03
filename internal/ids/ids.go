// Package ids generates short, sortable, collision-resistant identifiers.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// New returns prefix + yyyymmdd + "-" + 6 random hex chars, e.g. t20261003-a1b2c3.
func New(prefix string) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return prefix + time.Now().UTC().Format("20060102") + "-" + hex.EncodeToString(b[:])
}
