package jsonl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/core/session"
)

type savedListing struct {
	Version  int                 `json:"version"`
	Identity string              `json:"identity"`
	Size     int64               `json:"size"`
	Modified int64               `json:"modified"`
	Workdir  string              `json:"workdir"`
	Meta     session.SessionMeta `json:"meta"`
}

func (s *Store) listingPath(key string) string {
	hash := sha256.Sum256([]byte(key))
	return filepath.Join(s.root, "listing-cache", hex.EncodeToString(hash[:])+".json")
}
func (s *Store) loadListing(key, workdir string, sid session.SessionID, info os.FileInfo) (session.SessionMeta, bool) {
	identity := listingIdentity(info)
	if identity == "" {
		return session.SessionMeta{}, false
	}
	f, err := os.Open(s.listingPath(key))
	if err != nil {
		return session.SessionMeta{}, false
	}
	defer f.Close()
	var saved savedListing
	if err = json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&saved); err != nil {
		return session.SessionMeta{}, false
	}
	if saved.Version != 1 || saved.Identity != identity || saved.Size != info.Size() || saved.Modified != info.ModTime().UnixNano() || saved.Workdir != workdir || saved.Meta.ID != sid {
		return session.SessionMeta{}, false
	}
	return saved.Meta, true
}

// Optional acceleration. Cache write failure must not turn a successful log read into a failure.
// Return errors to the caller, which deliberately keeps the successful in-memory listing.
func (s *Store) saveListing(key, workdir string, info os.FileInfo, meta session.SessionMeta) error {
	identity := listingIdentity(info)
	if identity == "" {
		return nil
	}
	body, err := json.Marshal(savedListing{1, identity, info.Size(), info.ModTime().UnixNano(), workdir, meta})
	if err != nil {
		return err
	}
	path := s.listingPath(key)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return atomicfile.Write(path, body, 0600)
}
