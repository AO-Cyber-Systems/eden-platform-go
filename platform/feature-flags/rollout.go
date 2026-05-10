package featureflags

import (
	"crypto/sha256"
	"encoding/binary"
)

// inRollout returns true if (subjectID, salt) hashes into a bucket strictly
// below percentage, in the range [0, 100]. A percentage of 100 always returns
// true; 0 always returns false.
//
// The hash is deterministic across processes, hosts, and Go versions: it uses
// SHA-256 over salt + "\x00" + subjectID and reads the leading 4 bytes as a
// big-endian uint32, then maps that to [0, 100).
func inRollout(subjectID, salt string, percentage int) bool {
	if percentage <= 0 {
		return false
	}
	if percentage >= 100 {
		return true
	}
	h := sha256.New()
	h.Write([]byte(salt))
	h.Write([]byte{0})
	h.Write([]byte(subjectID))
	sum := h.Sum(nil)
	// Map first 4 bytes to a [0, 100) bucket.
	v := binary.BigEndian.Uint32(sum[:4])
	bucket := int(v % 100)
	return bucket < percentage
}
