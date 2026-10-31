package hashcache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRoundTripAndValidation(t *testing.T) {
	dir := t.TempDir()
	mod := time.Unix(1700000000, 123)
	h := [32]byte{1, 2, 3}

	c := Open(dir, "x", "p1")
	c.Put("/a", 10, mod, h)
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}

	c = Open(dir, "x", "p1")
	if got, ok := c.Get("/a", 10, mod); !ok || got != h {
		t.Fatalf("hit expected, got %v %v", got, ok)
	}
	if _, ok := c.Get("/a", 11, mod); ok {
		t.Error("size change must invalidate")
	}
	if _, ok := c.Get("/a", 10, mod.Add(time.Nanosecond)); ok {
		t.Error("mtime change must invalidate")
	}
	if _, ok := c.Get("/b", 10, mod); ok {
		t.Error("unknown path must miss")
	}
}

func TestParamsMismatchDiscards(t *testing.T) {
	dir := t.TempDir()
	c := Open(dir, "x", "p1")
	c.Put("/a", 1, time.Unix(1, 0), [32]byte{9})
	c.Flush()
	if Open(dir, "x", "p2").Len() != 0 {
		t.Error("different params must discard entries")
	}
}

func TestExpiry(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Now()
	c := open(dir, "x", "p", func() time.Time { return t0 })
	c.Put("/old", 1, t0, [32]byte{1})
	c.Flush()
	later := t0.Add(TTL + time.Hour)
	c = open(dir, "x", "p", func() time.Time { return later })
	if c.Len() != 0 {
		t.Error("entry older than TTL must be dropped")
	}
	// a hit within TTL keeps it alive
	c = open(dir, "x", "p", func() time.Time { return t0 })
	c.Put("/new", 1, t0, [32]byte{2})
	c.Flush()
	day6 := t0.Add(6 * 24 * time.Hour)
	c = open(dir, "x", "p", func() time.Time { return day6 })
	if _, ok := c.Get("/new", 1, t0); !ok {
		t.Fatal("expected hit")
	}
	c.Flush()
	day10 := t0.Add(10 * 24 * time.Hour)
	if open(dir, "x", "p", func() time.Time { return day10 }).Len() != 1 {
		t.Error("hit should refresh the TTL")
	}
}

func TestCorruptFileIsEmptyCache(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x"), []byte("garbage"), 0o600)
	c := Open(dir, "x", "p")
	if c.Len() != 0 {
		t.Fatal("corrupt cache must load empty")
	}
	c.Put("/a", 1, time.Unix(1, 0), [32]byte{1})
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}
	if Open(dir, "x", "p").Len() != 1 {
		t.Error("corrupt file should be replaced")
	}
}

func TestNilCache(t *testing.T) {
	var c *Cache
	c.Put("/a", 1, time.Now(), [32]byte{})
	if _, ok := c.Get("/a", 1, time.Now()); ok {
		t.Error("nil cache never hits")
	}
	if c.Flush() != nil {
		t.Error("nil flush is a no-op")
	}
}

func TestDataRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mod := time.Unix(1700000000, 5)
	c := Open(dir, "d", "p")
	c.PutData("/a", 3, mod, []byte{1, 2, 3})
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}
	c = Open(dir, "d", "p")
	if got, ok := c.GetData("/a", 3, mod); !ok || string(got) != "\x01\x02\x03" {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := c.GetData("/a", 4, mod); ok {
		t.Error("size change must invalidate")
	}
	if _, ok := c.GetData("/missing", 3, mod); ok {
		t.Error("miss expected")
	}
}
