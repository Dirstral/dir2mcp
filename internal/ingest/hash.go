package ingest

import (
	"crypto/sha256"
	"encoding/hex"
)

// computeContentHash computes a stable sha256 hash of file content.
// This is used for incremental indexing to detect if a document has changed.
func computeContentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// computeRepHash computes a stable sha256 hash of representation content.
// Historically this duplicated the logic of computeContentHash, but the
// algorithms are identical.  Delegate to computeContentHash so there’s a
// single authoritative implementation of the sha256+hex logic.
func computeRepHash(content []byte) string {
	return computeContentHash(content)
}

// needsReprocessing determines if a document needs to be reprocessed based on
// hash comparison. Returns true if the document should be reprocessed.
func needsReprocessing(oldHash, newHash string, forceReindex bool) bool {
	if forceReindex {
		return true
	}
	if oldHash == "" {
		// No existing hash means this is a new document
		return true
	}
	// Reprocess if hash has changed
	return oldHash != newHash
}
