// Package hashcache persists file hashes between scans in the OS user cache
// directory (macOS: ~/Library/Caches/veyra). An entry is keyed by path and
// is trusted only while the file's size and modification time still match.
// Entries unused for TTL are dropped when the cache is opened, so the file
// cannot grow without bound.
package hashcache

import (
	"bytes"
	"encoding/gob"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AppDir is the directory name under the OS cache directory.
const AppDir = "veyra"

// TTL is how long an unused entry survives.
const TTL = 7 * 24 * time.Hour

// Entry is one cached hash. Path is the key.
type Entry struct {
	Path    string
	ModTime int64 // UnixNano
	Size    int64
	Hash    [32]byte
	Data    []byte // opaque payload for non-hash caches (fingerprints, metadata)
	Used    int64  // Unix seconds of last hit or write; drives expiry
}

type header struct {
	Params  string // hash algorithm and sampling layout; a mismatch discards the file
	Entries []Entry
}

// Cache is a path-keyed hash cache. A nil *Cache is valid and caches nothing.
type Cache struct {
	mu      sync.Mutex
	file    string
	params  string
	entries map[string]Entry
	dirty   bool
	now     func() time.Time
}

// legacyAppDir is the directory used before the tool was renamed.
const legacyAppDir = "r.dedup"

// DefaultDir returns <user cache dir>/veyra. A cache left by the old name is
// moved over once, so a rename does not throw away hours of hashing.
func DefaultDir() (string, error) {
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(d, AppDir)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		_ = os.Rename(filepath.Join(d, legacyAppDir), dir) // best effort: a cache only saves work
	}
	return dir, nil
}

// Open loads dir/name. A missing, unreadable, corrupt or differently
// parameterised file yields an empty cache; it is never an error, because a
// cache only ever saves work.
func Open(dir, name, params string) *Cache { return open(dir, name, params, time.Now) }

func open(dir, name, params string, now func() time.Time) *Cache {
	c := &Cache{file: filepath.Join(dir, name), params: params, entries: make(map[string]Entry), now: now}
	data, err := os.ReadFile(c.file)
	if err != nil {
		return c
	}
	var h header
	if gob.NewDecoder(bytes.NewReader(data)).Decode(&h) != nil || h.Params != params {
		c.dirty = true // rewrite over the stale file on the next flush
		return c
	}
	cutoff := c.now().Add(-TTL).Unix()
	for _, e := range h.Entries {
		if e.Used >= cutoff {
			c.entries[e.Path] = e
		} else {
			c.dirty = true
		}
	}
	return c
}

// lookup returns the entry for path when its size and mtime still match.
func (c *Cache) lookup(path string, size int64, mod time.Time) (Entry, bool) {
	if c == nil {
		return Entry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[path]
	if !ok || e.Size != size || e.ModTime != mod.UnixNano() {
		return Entry{}, false
	}
	if now := c.now().Unix(); now-e.Used > 3600 { // refresh the TTL, but not on every hit
		e.Used = now
		c.entries[path] = e
		c.dirty = true
	}
	return e, true
}

func (c *Cache) store(e Entry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	e.Used = c.now().Unix()
	c.entries[e.Path] = e
	c.dirty = true
	c.mu.Unlock()
}

// Get returns the cached hash when path's size and mtime still match.
func (c *Cache) Get(path string, size int64, mod time.Time) ([32]byte, bool) {
	e, ok := c.lookup(path, size, mod)
	return e.Hash, ok
}

// Put records a freshly computed hash, replacing any stale entry for path.
func (c *Cache) Put(path string, size int64, mod time.Time, hash [32]byte) {
	c.store(Entry{Path: path, ModTime: mod.UnixNano(), Size: size, Hash: hash})
}

// GetData is Get for caches that hold an opaque payload. The slice must not be modified.
func (c *Cache) GetData(path string, size int64, mod time.Time) ([]byte, bool) {
	e, ok := c.lookup(path, size, mod)
	return e.Data, ok && e.Data != nil
}

// PutData is Put for an opaque payload.
func (c *Cache) PutData(path string, size int64, mod time.Time, data []byte) {
	c.store(Entry{Path: path, ModTime: mod.UnixNano(), Size: size, Data: data})
}

// Len is the number of live entries.
func (c *Cache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Flush writes the cache atomically (temp file in the same directory, then
// rename) if anything changed. Safe to call repeatedly and after cancellation.
func (c *Cache) Flush() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return nil
	}
	h := header{Params: c.params, Entries: make([]Entry, 0, len(c.entries))}
	for _, e := range c.entries {
		h.Entries = append(h.Entries, e)
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&h); err != nil {
		return err
	}
	dir := filepath.Dir(c.file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(c.file)+".tmp*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(buf.Bytes())
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(tmp.Name(), c.file); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	c.dirty = false
	return nil
}
