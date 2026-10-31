package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RishiKendai/veyra/internal/model"
)

func TestParse(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	spaced := filepath.Join(dir, "my movies ünïcode 日本")
	if err := os.Mkdir(spaced, 0o700); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		args    []string
		wantErr string // substring; empty = success
		check   func(*testing.T, Options)
	}{
		{"help alone", []string{"-h"}, "", func(t *testing.T, o Options) {
			if !o.Help {
				t.Error("Help not set")
			}
		}},
		{"no args is interactive", nil, "", func(t *testing.T, o Options) {
			if !o.Interactive || o.Root != "" {
				t.Errorf("got %+v", o)
			}
		}},
		{"help word", []string{"help"}, "", func(t *testing.T, o Options) {
			if !o.Help {
				t.Error("Help not set")
			}
		}},
		{"version", []string{"version"}, "", func(t *testing.T, o Options) {
			if !o.Version {
				t.Error("Version not set")
			}
		}},
		{"help with other args is not help", []string{"help", "me"}, "missing required flag -t", nil},
		{"video", []string{"-t", "video", dir}, "", func(t *testing.T, o Options) {
			if o.Type != model.Video || o.Recursive || o.Root != dir {
				t.Errorf("got %+v", o)
			}
		}},
		{"image recursive", []string{"-t", "image", "-r", dir}, "", func(t *testing.T, o Options) {
			if o.Type != model.Image || !o.Recursive {
				t.Errorf("got %+v", o)
			}
		}},
		{"hidden off by default", []string{"-t", "image", dir}, "", func(t *testing.T, o Options) {
			if o.Hidden {
				t.Error("Hidden must default to false")
			}
		}},
		{"-a includes hidden", []string{"-t", "image", "-r", "-a", dir}, "", func(t *testing.T, o Options) {
			if !o.Hidden || !o.Recursive {
				t.Errorf("got %+v", o)
			}
		}},
		{"-a after the path", []string{dir, "-t", "video", "-a"}, "", func(t *testing.T, o Options) {
			if !o.Hidden {
				t.Errorf("got %+v", o)
			}
		}},
		{"flags after path", []string{dir, "-r", "-t", "image"}, "", nil},
		{"spaces and unicode", []string{"-t", "image", spaced}, "", func(t *testing.T, o Options) {
			if o.Root != spaced {
				t.Errorf("root = %q", o.Root)
			}
		}},
		{"missing -t", []string{dir}, "missing required flag -t", nil},
		{"-t without value", []string{"-t"}, "requires a value", nil},
		{"invalid type", []string{"-t", "audio", dir}, "invalid type", nil},
		{"type file rejected", []string{"-t", "file", dir}, "invalid type", nil},
		{"missing path", []string{"-t", "video"}, "missing directory", nil},
		{"nonexistent", []string{"-t", "video", filepath.Join(dir, "nope")}, "does not exist", nil},
		{"path is file", []string{"-t", "video", file}, "not a directory", nil},
		{"extra args", []string{"-t", "video", dir, dir}, "extra arguments", nil},
		{"unknown flag", []string{"-t", "video", "--json", dir}, "unknown flag", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := Parse(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, o)
			}
		})
	}
}

func TestParseUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("root can read everything")
	}
	d := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(d, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(d, 0o700) })
	if _, err := Parse([]string{"-t", "image", d}); err == nil || !strings.Contains(err.Error(), "not readable") {
		t.Fatalf("err = %v, want not readable", err)
	}
}

// "veyra -t image ." scans the current folder.
func TestParseDotIsCurrentFolder(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-t", "image", "."}, {".", "-t", "image"}, {"-t", "image", "-r", "."}} {
		o, err := Parse(args)
		if err != nil || o.Root != wd {
			t.Errorf("Parse(%v) = %+v, %v; want root %q", args, o, err, wd)
		}
	}
}
