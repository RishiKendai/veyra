package scanner

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/RishiKendai/veyra/internal/classify"
)

func scanHidden(t *testing.T, root string, hidden bool) []string {
	t.Helper()
	res, err := Scan(context.Background(), Options{Root: root, Recursive: true, Hidden: hidden, Classify: classify.Image, Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	return relNames(t, root, res)
}

// hiddenTree has visible and hidden images at the top level, in a visible
// folder and inside a hidden folder.
func hiddenTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range []string{
		"a.png", ".dot.png", "._a.png",
		"sub/b.png", "sub/.c.png",
		".hiddendir/d.png", ".hiddendir/deeper/e.png",
	} {
		write(t, filepath.Join(root, filepath.FromSlash(p)), pngBytes(t))
	}
	return root
}

func TestHiddenFilesAndFoldersSkippedByDefault(t *testing.T) {
	got := scanHidden(t, hiddenTree(t), false)
	want := []string{"a.png", filepath.Join("sub", "b.png")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

func TestHiddenIncludedWhenAsked(t *testing.T) {
	got := scanHidden(t, hiddenTree(t), true)
	want := []string{
		"._a.png", ".dot.png",
		filepath.Join(".hiddendir", "d.png"), filepath.Join(".hiddendir", "deeper", "e.png"),
		"a.png", filepath.Join("sub", ".c.png"), filepath.Join("sub", "b.png"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

// Hidden folders are not entered, so their contents are not even counted as examined.
func TestHiddenFoldersAreNotEntered(t *testing.T) {
	root := hiddenTree(t)
	var prog Progress
	if _, err := Scan(context.Background(), Options{Root: root, Recursive: true, Classify: classify.Image, Workers: 2, Progress: &prog}); err != nil {
		t.Fatal(err)
	}
	if got := prog.Seen.Load(); got != 2 {
		t.Errorf("examined %d files, want 2 (a.png and sub/b.png)", got)
	}
}

// The folder the user points at is always scanned, even if its own name is hidden.
func TestHiddenRootIsStillScanned(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".photos")
	write(t, filepath.Join(root, "a.png"), pngBytes(t))
	write(t, filepath.Join(root, ".b.png"), pngBytes(t))
	if got := scanHidden(t, root, false); !reflect.DeepEqual(got, []string{"a.png"}) {
		t.Fatalf("files = %v, want [a.png]", got)
	}
}

func TestHiddenNotRecursive(t *testing.T) {
	root := hiddenTree(t)
	res, err := Scan(context.Background(), Options{Root: root, Hidden: false, Classify: classify.Image, Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := relNames(t, root, res); !reflect.DeepEqual(got, []string{"a.png"}) {
		t.Fatalf("files = %v, want [a.png]", got)
	}
}
