package ids

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

func NewTraceID() string {
	return newHexID("tr")
}

func newHexID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err == nil {
		return prefix + "-" + hex.EncodeToString(buf)
	}
	return prefix + "-" + hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
}
