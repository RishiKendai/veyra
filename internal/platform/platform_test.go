package platform

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type call struct {
	stdin string
	argv  []string
}

func fake(calls *[]call, err error) *Opener { return fakeOS("darwin", nil, calls, err) }

// fakeOS records the commands an Opener for goos would run. have lists the
// executables that "exist" on the pretend machine.
func fakeOS(goos string, have []string, calls *[]call, err error) *Opener {
	return &Opener{ctx: context.Background(), goos: goos,
		run: func(_ context.Context, stdin, name string, args ...string) error {
			*calls = append(*calls, call{stdin, append([]string{name}, args...)})
			return err
		},
		lookPath: func(n string) (string, error) {
			for _, h := range have {
				if h == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		}}
}

func tempFile(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCommandsUseArgumentArrays(t *testing.T) {
	// A hostile name must reach the command as a single, intact argument.
	p := tempFile(t, "a b; rm -rf ~ $(touch pwned) `x` \"q\" 日本.mp4")
	var calls []call
	o := fake(&calls, nil)
	if err := o.Open(p); err != nil {
		t.Fatal(err)
	}
	if err := o.Reveal(p); err != nil {
		t.Fatal(err)
	}
	if err := o.QuickLook(p); err != nil {
		t.Fatal(err)
	}
	if err := o.Copy(p); err != nil {
		t.Fatal(err)
	}
	want := []call{
		{"", []string{"open", p}},
		{"", []string{"open", "-R", p}},
		{"", []string{"qlmanage", "-p", p}},
		{p, []string{"pbcopy"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v\nwant    %#v", calls, want)
	}
}

func TestRejectsBadPaths(t *testing.T) {
	var calls []call
	o := fake(&calls, nil)
	for name, p := range map[string]string{
		"relative":       "movie.mp4",
		"option-looking": "-R",
		"empty":          "",
		"nul":            "/tmp/a\x00b",
		"missing":        filepath.Join(t.TempDir(), "gone.mp4"),
	} {
		if err := o.Open(p); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("no command may run for invalid paths, got %v", calls)
	}
	err := o.Reveal(filepath.Join(t.TempDir(), "gone.mp4"))
	if err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("vanished file error = %v", err)
	}
}

func TestFailuresAreReported(t *testing.T) {
	p := tempFile(t, "a.mp4")
	var calls []call
	o := fake(&calls, errors.New("boom"))
	for name, err := range map[string]error{"open": o.Open(p), "reveal": o.Reveal(p), "copy": o.Copy("x"), "ql": o.QuickLook(p)} {
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}

func TestQuickLookStopsWithContext(t *testing.T) {
	p := tempFile(t, "a.mp4")
	ctx, cancel := context.WithCancel(context.Background())
	o := &Opener{ctx: ctx, goos: "darwin", run: func(ctx context.Context, _, _ string, _ ...string) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	done := make(chan error)
	go func() { done <- o.QuickLook(p) }()
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("quitting while Quick Look is open should not be an error, got %v", err)
	}
}

func TestExecRunReportsStderr(t *testing.T) {
	err := execRun(context.Background(), "", "sh", "-c", "echo 'The file does not exist.' >&2; exit 1")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v", err)
	}
}

func TestTrashPassesPathAsArgumentNotScriptText(t *testing.T) {
	hostile := tempFile(t, `a" & (do shell script "touch pwned") & ".mp4`)
	var calls []call
	o := fake(&calls, nil)
	if err := o.Trash(hostile); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].argv[0] != "osascript" {
		t.Fatalf("calls = %#v", calls)
	}
	argv := calls[0].argv
	if argv[len(argv)-1] != hostile {
		t.Fatalf("path must be the final argument, got %q", argv[len(argv)-1])
	}
	for _, a := range argv[:len(argv)-1] {
		if strings.Contains(a, "pwned") || strings.Contains(a, hostile) {
			t.Fatalf("the path leaked into script text: %q", a)
		}
	}
}

func TestTrashErrors(t *testing.T) {
	p := tempFile(t, "a.mp4")
	var calls []call
	if err := fake(&calls, errors.New("execution error: Not authorized to send Apple events to Finder. (-1743)")).Trash(p); err == nil ||
		!strings.Contains(err.Error(), "Automation") {
		t.Fatalf("permission failure should say how to fix it, got %v", err)
	}
	if err := fake(&calls, errors.New("volume is read only")).Trash(p); err == nil || !strings.Contains(err.Error(), "read only") {
		t.Fatalf("other failures must be passed through, got %v", err)
	}
	calls = nil
	if err := fake(&calls, nil).Trash(filepath.Join(t.TempDir(), "gone")); err == nil || len(calls) != 0 {
		t.Fatalf("missing files must be rejected before any command runs: %v %v", err, calls)
	}
	if err := fake(&calls, nil).Trash("relative.mp4"); err == nil || len(calls) != 0 {
		t.Fatal("relative paths must be refused")
	}
}

// TestRealTrash moves a throwaway file to the real Trash through Finder. It is
// opt-in because it needs Automation permission and touches the user's Trash:
//
//	VEYRA_REAL_TRASH=1 go test -run RealTrash ./internal/platform
func TestRealTrash(t *testing.T) {
	if os.Getenv("VEYRA_REAL_TRASH") == "" {
		t.Skip("set VEYRA_REAL_TRASH=1 to run")
	}
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, fmt.Sprintf("veyra-trash-test-%d.txt", os.Getpid()))
	if err := os.WriteFile(p, []byte("throwaway"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(p) })
	if err := New(context.Background()).Trash(p); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("file should be gone from its folder, stat err = %v", err)
	}
	trashed := filepath.Join(home, ".Trash", filepath.Base(p))
	if _, err := os.Stat(trashed); err != nil {
		t.Fatalf("file should be in ~/.Trash: %v", err)
	}
	t.Logf("moved to %s", trashed)
	os.Remove(trashed) // clean up our own test file
}

func TestLinuxCommands(t *testing.T) {
	p := tempFile(t, "a b.mp4")
	uri := "file://" + strings.ReplaceAll(p, " ", "%20")
	var calls []call
	o := fakeOS("linux", []string{"dbus-send", "gio", "xclip"}, &calls, nil)
	for _, err := range []error{o.Open(p), o.Reveal(p), o.Trash(p), o.Copy(p)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []call{
		{"", []string{"xdg-open", p}},
		{"", []string{"dbus-send", "--session", "--print-reply", "--dest=org.freedesktop.FileManager1",
			"/org/freedesktop/FileManager1", "org.freedesktop.FileManager1.ShowItems", "array:string:" + uri, "string:"}},
		{"", []string{"gio", "trash", p}},
		{p, []string{"xclip", "-selection", "clipboard"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v\nwant    %#v", calls, want)
	}
	if o.CanQuickLook() || o.QuickLook(p) == nil {
		t.Error("Quick Look must be unavailable on Linux")
	}
}

func TestLinuxFallbacks(t *testing.T) {
	p := tempFile(t, "a.mp4")
	var calls []call
	o := fakeOS("linux", nil, &calls, nil)
	if err := o.Trash(p); err == nil || !strings.Contains(err.Error(), "no Trash tool") {
		t.Errorf("Trash without tools: %v", err)
	}
	if err := o.Copy("x"); err == nil || !strings.Contains(err.Error(), "no clipboard tool") {
		t.Errorf("Copy without tools: %v", err)
	}
	if err := o.Reveal(p); err != nil || len(calls) != 1 || !reflect.DeepEqual(calls[0].argv, []string{"xdg-open", filepath.Dir(p)}) {
		t.Errorf("Reveal without dbus should open the folder: %v %#v", err, calls)
	}
	calls = nil
	if err := fakeOS("linux", []string{"trash-put"}, &calls, nil).Trash(p); err != nil || calls[0].argv[0] != "trash-put" {
		t.Errorf("trash-put fallback: %v %#v", err, calls)
	}
}

func TestWindowsCommands(t *testing.T) {
	p := tempFile(t, "a b; calc & `x` $(y).mp4")
	var calls []call
	o := fakeOS("windows", nil, &calls, nil)
	for _, err := range []error{o.Open(p), o.Reveal(p), o.Trash(p), o.Copy("naïve 日本")} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(calls[0].argv, []string{"rundll32", "url.dll,FileProtocolHandler", p}) {
		t.Errorf("open: %#v", calls[0])
	}
	if !reflect.DeepEqual(calls[1].argv, []string{"explorer", "/select," + p}) {
		t.Errorf("reveal: %#v", calls[1])
	}
	// The path to trash and the text to copy must travel as base64 on stdin, never in script text.
	if calls[2].argv[0] != "powershell" || calls[2].stdin != base64.StdEncoding.EncodeToString([]byte(p))+"\n" {
		t.Errorf("trash: %#v", calls[2])
	}
	for _, c := range calls[2:] {
		for _, a := range c.argv {
			if strings.Contains(a, "calc") || strings.Contains(a, "日本") {
				t.Errorf("data leaked into the script: %q", a)
			}
		}
	}
	if calls[3].stdin != base64.StdEncoding.EncodeToString([]byte("naïve 日本"))+"\n" {
		t.Errorf("copy stdin = %q", calls[3].stdin)
	}
}

func TestLabels(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "Finder", "windows": "Explorer", "linux": "file manager"} {
		var calls []call
		if got := fakeOS(goos, nil, &calls, nil).RevealLabel(); got != want {
			t.Errorf("%s: label %q, want %q", goos, got, want)
		}
	}
}
