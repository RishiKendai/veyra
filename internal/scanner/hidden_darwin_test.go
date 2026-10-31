package scanner

import (
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// A file or folder flagged hidden with chflags has no leading dot but Finder
// hides it, so it must be skipped too.
func TestMacHiddenFlagIsHonoured(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "shown.png"), pngBytes(t))
	write(t, filepath.Join(root, "flagged.png"), pngBytes(t))
	write(t, filepath.Join(root, "flaggeddir", "x.png"), pngBytes(t))
	for _, p := range []string{"flagged.png", "flaggeddir"} {
		if err := unix.Chflags(filepath.Join(root, p), 0x8000); err != nil { // UF_HIDDEN
			t.Skipf("cannot set the hidden flag here: %v", err)
		}
	}
	if got := scanHidden(t, root, false); !reflect.DeepEqual(got, []string{"shown.png"}) {
		t.Fatalf("hidden off: files = %v, want [shown.png]", got)
	}
	want := []string{"flagged.png", filepath.Join("flaggeddir", "x.png"), "shown.png"}
	if got := scanHidden(t, root, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("hidden on: files = %v, want %v", got, want)
	}
}
