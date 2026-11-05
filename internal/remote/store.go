package remote

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Store is where a vault is mirrored. Paths are slash-separated paths
// relative to the vault root, the same keys the vault itself uses.
type Store interface {
	// PutFile uploads the file at path as rel.
	PutFile(ctx context.Context, rel, path string) error
	// Delete removes rel from the remote. Deleting a file that is not
	// there is not an error.
	Delete(ctx context.Context, rel string) error
	// Ping checks that the remote accepts writes.
	Ping(ctx context.Context) error
	// Target names this remote, so a manifest for another remote is not
	// reused.
	Target() string
	// Describe names the remote for people.
	Describe() string
}

// checkRel checks a vault-relative path before a store turns it into an
// object key or a file path.
func checkRel(rel string) error {
	if rel == "" {
		return errors.New("remote: empty path")
	}
	if strings.HasPrefix(rel, "/") || strings.Contains(rel, `\`) {
		return errors.New("remote: invalid path " + rel)
	}
	for _, part := range strings.Split(rel, "/") {
		// A segment may begin with a dot: a knowledge base keeps its
		// pictures and attachments in .assets, which is synced too.
		if part == "" || part == "." || part == ".." || strings.ContainsRune(part, 0) {
			return errors.New("remote: invalid path " + rel)
		}
	}
	return nil
}

// putTimeout is how long one request or copy may take.
const putTimeout = 2 * time.Minute
