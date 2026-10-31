package app

import (
	"os"
	"testing"
)

// TestMain points the user cache dir at a temp dir so tests never read or
// write the real ~/Library/Caches/veyra.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "veyra-home")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
