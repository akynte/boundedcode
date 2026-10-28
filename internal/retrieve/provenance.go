package retrieve

import (
	"crypto/sha1" //nolint:gosec // git's object id, not a security boundary
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Provenance says where a retrieved item came from, so that a later
// reader (the model after more turns, a resumed session) can tell when it
// went stale.
type Provenance struct {
	// Revision is the commit checked out when it was retrieved.
	Revision string `json:"revision,omitempty"`
	// Blob is the git blob id of the file's content as read (the id git
	// would give it, whether or not it is committed).
	Blob string `json:"blob,omitempty"`
	// Methods are the strategies that found it.
	Methods []string `json:"methods"`
	// At is when it was retrieved.
	At time.Time `json:"at"`
}

// BlobID is the git blob id of content.
func BlobID(content []byte) string {
	h := sha1.New() //nolint:gosec // git's object id
	h.Write([]byte("blob " + strconv.Itoa(len(content)) + "\x00"))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// FileBlob is the blob id of root/rel now ("" when it cannot be read: it
// was deleted or replaced by something else).
func FileBlob(root, rel string) string {
	p := filepath.Join(root, filepath.FromSlash(rel))
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return BlobID(b)
}

// stamp records each item's provenance, and marks the items whose content
// the model already has (Known with the same blob).
func stamp(r *repo, items []Item, rev string, known map[string]string) {
	now := time.Now().UTC()
	for i := range items {
		it := &items[i]
		seen := map[string]bool{}
		for _, w := range it.Why {
			if !seen[string(w.Strategy)] {
				seen[string(w.Strategy)] = true
				it.Prov.Methods = append(it.Prov.Methods, string(w.Strategy))
			}
		}
		it.Prov.Revision, it.Prov.At = rev, now
		if b, ok := r.read(it.File); ok {
			it.Prov.Blob = BlobID(b)
		}
		if k, ok := known[it.File]; ok && (k == "" || k == it.Prov.Blob) {
			it.Known = true
		}
	}
}

// Stale lists the items whose file changed since they were retrieved
// (another blob, or gone), relative to root.
func Stale(root string, items []Item) []string {
	var out []string
	for _, it := range items {
		if it.Prov.Blob != "" && FileBlob(root, it.File) != it.Prov.Blob {
			out = append(out, it.File)
		}
	}
	return out
}
