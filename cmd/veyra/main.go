package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/RishiKendai/veyra/internal/app"
	"github.com/RishiKendai/veyra/internal/cli"
	"github.com/RishiKendai/veyra/internal/menu"
	"github.com/RishiKendai/veyra/internal/model"
	"github.com/RishiKendai/veyra/internal/platform"
	"github.com/RishiKendai/veyra/internal/prof"
	"github.com/RishiKendai/veyra/internal/progress"
	"github.com/RishiKendai/veyra/internal/report"
	"github.com/RishiKendai/veyra/internal/scanner"
	"github.com/RishiKendai/veyra/internal/tui"
)

// processStart is when the program began; the "total time" shown to the user
// is measured from here, so it covers everything, not just one stage.
var processStart = time.Now()

// version is set at release time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() { os.Exit(run()) }

func run() int {
	opts, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "veyra: %v\n\nRun 'veyra help' for usage.\n", err)
		return 2
	}
	switch {
	case opts.Help:
		fmt.Print(cli.Usage)
		return 0
	case opts.Version:
		fmt.Printf("veyra %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.Interactive {
		// Without a terminal there is nobody to ask: behave like a plain command.
		if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
			fmt.Fprint(os.Stderr, cli.Usage)
			return 2
		}
		start, _ := os.Getwd()
		res, err := menu.Run(ctx, start, checkDeps)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "veyra: %v\n", err)
			return 1
		case res.Quit:
			return 0
		}
		opts = cli.Options{Type: res.Type, Recursive: res.Recursive, Hidden: res.Hidden, Root: res.Root}
	}
	if err := checkDeps(opts.Type); err != nil {
		fmt.Fprintf(os.Stderr, "veyra: %v\n", err)
		return 1
	}

	// Fingerprints are tiny; keep garbage-collection overhead low but the
	// footprint bounded.
	debug.SetGCPercent(200)
	debug.SetMemoryLimit(1 << 30)

	stopProfile := prof.StartCPUProfile()
	defer stopProfile()
	defer prof.Report(os.Stderr)

	prog := new(scanner.Progress)
	tracker := progress.New(string(opts.Type) + "s")
	done := make(chan struct{})
	stderrIsTTY := isTerminal(os.Stderr)
	if stderrIsTTY {
		go showProgress(os.Stderr, tracker, prog, done)
	}
	out, err := app.Run(ctx, app.Options{
		Type: opts.Type, Recursive: opts.Recursive, Hidden: opts.Hidden, Root: opts.Root, Progress: prog,
		Report: tracker.Set,
		OnVolume: func(down bool) {
			if down {
				tracker.SetNote("Drive disconnected: waiting for " + opts.Root + " to come back (Ctrl+C to stop)")
			} else {
				tracker.SetNote("")
			}
		},
	})
	close(done)
	if stderrIsTTY {
		<-showProgressDone // the status line is erased before anything else is printed
	}

	for _, s := range out.Skipped {
		fmt.Fprintf(os.Stderr, "veyra: skipped %s: %s\n", s.Path, s.Reason)
	}
	switch {
	case errors.Is(err, context.Canceled):
		fmt.Fprintln(os.Stderr, "veyra: interrupted; results are partial")
		return 130
	case err != nil:
		fmt.Fprintf(os.Stderr, "veyra: scan failed: %v\n", err)
		return 1
	}
	if out.Outages > 0 {
		fmt.Fprintf(os.Stderr, "veyra: the drive disconnected %d time(s) during the scan; veyra waited for it and carried on\n", out.Outages)
	}
	if out.Partial {
		fmt.Fprintln(os.Stderr, "veyra: warning: scan was partial (some paths could not be read)")
	}

	total := time.Since(processStart)

	// Interactive only when both ends are a terminal and there is something to review.
	if len(out.Groups) == 0 || !isTerminal(os.Stdout) || !isTerminal(os.Stdin) {
		report.Write(os.Stdout, report.Info{Type: opts.Type, Examined: out.Examined, Scanned: out.Scanned, Elapsed: total}, out.Groups)
		return 0
	}
	opener := platform.New(ctx)
	err = tui.Run(ctx, tui.Input{
		Groups: out.Groups, Root: out.Root, Type: opts.Type,
		Examined: out.Examined, Scanned: out.Scanned, Skipped: len(out.Skipped), Partial: out.Partial, Elapsed: total,
	}, opener)
	stop() // cancels the Opener's context: Quick Look and other children die with us
	if err != nil {
		fmt.Fprintf(os.Stderr, "veyra: %v\n", err)
		return 1
	}
	return 0
}

// checkDeps fails early with an install hint when a required tool is missing.
func checkDeps(t model.MediaType) error {
	if t != model.Video {
		return nil
	}
	for _, tool := range []string{"ffprobe", "ffmpeg"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s not found in PATH; video scanning needs FFmpeg (install with: %s)", tool, ffmpegHint())
		}
	}
	return nil
}

// ffmpegHint is the usual way to install FFmpeg on this OS.
func ffmpegHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install ffmpeg"
	case "windows":
		return "winget install Gyan.FFmpeg"
	}
	return "sudo apt install ffmpeg, or your distribution's package manager"
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// showProgressDone is closed when showProgress has erased its line.
var showProgressDone = make(chan struct{})

// showProgress redraws one status line a few times a second. The line is cut
// to the terminal width, because a wrapped line cannot be redrawn with \r and
// would scroll garbage down the screen.
func showProgress(w *os.File, t *progress.Tracker, scan *scanner.Progress, done <-chan struct{}) {
	defer close(showProgressDone)
	tick := time.NewTicker(80 * time.Millisecond) // matches the loader frame rate
	defer tick.Stop()
	for {
		select {
		case <-done:
			fmt.Fprint(w, "\r\033[K")
			return
		case <-tick.C:
			t.SetScan(scan.Seen.Load(), scan.Matched.Load())
			width := 80
			if cols, _, err := term.GetSize(int(w.Fd())); err == nil && cols > 1 {
				width = cols
			}
			fmt.Fprintf(w, "\r\033[K%s", ansi.Truncate(t.Line(time.Now()), width-1, "…"))
		}
	}
}
