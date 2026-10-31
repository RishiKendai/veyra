// Package platform performs the native desktop actions veyra needs (open a
// file, reveal it in the file manager, move it to the Trash, copy text) on
// macOS, Linux and Windows. Every command is started with an explicit
// argument list and never through a shell; paths are treated as untrusted data.
package platform

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	actionTimeout = 10 * time.Second
	trashTimeout  = 60 * time.Second
)

// runFunc starts name with args, optionally feeding stdin, and returns once
// the process exits. It is a field so tests can observe calls without
// launching applications.
type runFunc func(ctx context.Context, stdin string, name string, args ...string) error

// Opener performs native actions. Its context bounds long-running children
// (Quick Look): cancelling it kills them. The command set is chosen by goos,
// which is a field so every platform's argument lists can be tested anywhere.
type Opener struct {
	ctx      context.Context
	run      runFunc
	goos     string
	lookPath func(string) (string, error)
}

// New returns an Opener for the current OS whose child processes die with ctx.
func New(ctx context.Context) *Opener {
	return &Opener{ctx: ctx, run: execRun, goos: runtime.GOOS, lookPath: exec.LookPath}
}

// RevealLabel names the file manager, for menus and help text.
func (o *Opener) RevealLabel() string {
	switch o.goos {
	case "darwin":
		return "Finder"
	case "windows":
		return "Explorer"
	}
	return "file manager"
}

// RestoreHint tells the user how a trashed file comes back.
func (o *Opener) RestoreHint() string {
	switch o.goos {
	case "darwin":
		return "Finder's Put Back works"
	case "windows":
		return "restore it from the Recycle Bin"
	}
	return "restore it from the Trash"
}

// CanQuickLook reports whether a native preview window is available.
func (o *Opener) CanQuickLook() bool { return o.goos == "darwin" }

// Open opens path in its default application.
func (o *Opener) Open(path string) error {
	if err := checkFile(path); err != nil {
		return err
	}
	switch o.goos {
	case "darwin":
		return o.bounded("open", path)
	case "windows":
		return o.bounded("rundll32", "url.dll,FileProtocolHandler", path)
	}
	return o.bounded("xdg-open", path)
}

// Reveal shows path selected in the file manager.
func (o *Opener) Reveal(path string) error {
	if err := checkFile(path); err != nil {
		return err
	}
	switch o.goos {
	case "darwin":
		return o.bounded("open", "-R", path)
	case "windows":
		// explorer.exe reports exit status 1 even when it succeeded.
		err := o.bounded("explorer", "/select,"+path)
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil
		}
		return err
	}
	// Ask the desktop's file manager to select the file; without one, open its folder.
	if _, err := o.lookPath("dbus-send"); err == nil {
		uri := (&url.URL{Scheme: "file", Path: path}).String()
		if o.bounded("dbus-send", "--session", "--print-reply", "--dest=org.freedesktop.FileManager1",
			"/org/freedesktop/FileManager1", "org.freedesktop.FileManager1.ShowItems",
			"array:string:"+uri, "string:") == nil {
			return nil
		}
	}
	return o.bounded("xdg-open", filepath.Dir(path))
}

// QuickLook previews path with qlmanage -p (macOS only). It blocks until the
// preview window is closed or the Opener's context is cancelled.
func (o *Opener) QuickLook(path string) error {
	if !o.CanQuickLook() {
		return errors.New("Quick Look is only available on macOS")
	}
	if err := checkFile(path); err != nil {
		return err
	}
	err := o.run(o.ctx, "", "qlmanage", "-p", path)
	if o.ctx.Err() != nil {
		return nil // veyra is quitting; the window was closed on purpose
	}
	return wrap("qlmanage", err)
}

// Trash moves path to the Trash / Recycle Bin, never a permanent delete. The
// path always travels as data (an argv entry, or base64 on stdin), never
// inside script text, so no character in it can be interpreted.
func (o *Opener) Trash(path string) error {
	if err := checkFile(path); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(o.ctx, trashTimeout)
	defer cancel()
	switch o.goos {
	case "darwin":
		// Finder uses the Trash of the file's own volume and supports "Put Back".
		// It asks for Automation permission the first time.
		err := o.run(ctx, "", "osascript",
			"-e", "on run argv",
			"-e", `tell application "Finder" to delete (POSIX file (item 1 of argv) as alias)`,
			"-e", "end run",
			path)
		if err != nil {
			if strings.Contains(err.Error(), "-1743") {
				return errors.New("macOS blocked this: allow your terminal to control Finder in System Settings → Privacy & Security → Automation")
			}
			return wrap("Finder", err)
		}
		return nil
	case "windows":
		const script = `$p=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadLine()));` +
			`Add-Type -AssemblyName Microsoft.VisualBasic;` +
			`[Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile($p,` +
			`[Microsoft.VisualBasic.FileIO.UIOption]::OnlyErrorDialogs,` +
			`[Microsoft.VisualBasic.FileIO.RecycleOption]::SendToRecycleBin)`
		return wrap("Recycle Bin", o.run(ctx, encode(path), "powershell", "-NoProfile", "-NonInteractive", "-Command", script))
	}
	for _, t := range [][]string{{"gio", "trash"}, {"trash-put"}, {"trash"}} {
		if _, err := o.lookPath(t[0]); err == nil {
			return wrap(t[0], o.run(ctx, "", t[0], append(t[1:], path)...))
		}
	}
	return errors.New("no Trash tool found: install gio (glib) or trash-cli")
}

// Copy puts text on the clipboard.
func (o *Opener) Copy(text string) error {
	switch o.goos {
	case "darwin":
		return o.bounded2(text, "pbcopy")
	case "windows":
		return o.bounded2(encode(text), "powershell", "-NoProfile", "-NonInteractive", "-Command",
			`Set-Clipboard -Value ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadLine())))`)
	}
	for _, t := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
		if _, err := o.lookPath(t[0]); err == nil {
			return o.bounded2(text, t[0], t[1:]...)
		}
	}
	return errors.New("no clipboard tool found: install wl-clipboard, xclip or xsel")
}

// encode makes text safe to hand to PowerShell on stdin whatever the console code page is.
func encode(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) + "\n" }

func (o *Opener) bounded(name string, args ...string) error { return o.bounded2("", name, args...) }

func (o *Opener) bounded2(stdin, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(o.ctx, actionTimeout)
	defer cancel()
	return wrap(name, o.run(ctx, stdin, name, args...))
}

// checkFile rejects anything that is not an absolute path to an existing file,
// so the argument can never be read as an option and a vanished file produces
// a clear message instead of a Finder dialog.
func checkFile(path string) error {
	switch {
	case path == "" || strings.ContainsRune(path, 0):
		return errors.New("invalid path")
	case !filepath.IsAbs(path):
		return fmt.Errorf("refusing relative path %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file no longer exists: %s", path)
		}
		return err
	}
	return nil
}

func wrap(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", name, err)
}

func execRun(ctx context.Context, stdin string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		if msg := firstLine(out.String()); msg != "" {
			return fmt.Errorf("%s (%w)", msg, err)
		}
		return err
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
