package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Digest returns a stable SHA-256 content digest of v. It is the single
// primitive used to build idempotency keys, lock snapshots, influence-domain
// summaries and evidence content digests. Marshalling uses encoding/json, whose
// output for maps is sorted by key, so digests are reproducible regardless of
// construction order.
func Digest(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Digest is only ever called on plain data or structs of plain data,
		// which cannot fail to marshal. A failure here is a programming error.
		panic("domain: digest marshal: " + err.Error())
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
