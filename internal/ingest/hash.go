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
// This is used for incremental indexing to detect if a representation has changed.
func computeRepHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
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
