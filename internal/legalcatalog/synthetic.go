package legalcatalog

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// SyntheticProvisionIDPrefix starts the provision ids WeRAG makes up for a
// provision without a vbpl.vn id: a document no KB holds, a copy whose
// chunks carry no portal ids (a legal feed's text), or an unresolved
// reference. Such an id names a (document, path) pair, never a portal id.
const SyntheticProvisionIDPrefix = "fp-"

// SyntheticProvisionID names the provision at path of document docID:
// stable, and short enough for legal_provision_links (varchar 64). It embeds
// docID, so a document that changes id gets new synthetic ids.
func SyntheticProvisionID(docID, path string) string {
	sum := sha1.Sum([]byte(docID + "#" + path))
	return SyntheticProvisionIDPrefix + hex.EncodeToString(sum[:])[:24]
}

// IsSyntheticProvisionID reports whether id was made by SyntheticProvisionID.
func IsSyntheticProvisionID(id string) bool {
	return strings.HasPrefix(id, SyntheticProvisionIDPrefix)
}
