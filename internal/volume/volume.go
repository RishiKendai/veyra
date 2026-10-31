// Package volume lets a scan survive an external drive that disconnects.
//
// USB drives drop off the bus (a power dip, a flaky cable, or heavy seeking on
// a bus-powered disk) and macOS unmounts them; a minute later they are back.
// Without a guard every read in between fails at once, each file is recorded
// as skipped and the scan "finishes" almost empty. A Guard notices that the
// volume itself is gone, pauses all work until it returns, and retries the
// operation that failed, so the scan covers the whole disk.
package volume

import (
	"context"
	"errors"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Guard is safe for concurrent use. A nil *Guard does nothing, so callers need
// not special-case internal disks.
type Guard struct {
	root   string
	probe  func() bool   // reports whether the volume is mounted and reachable
	poll   time.Duration // how often to check while it is gone
	settle time.Duration // pause after it returns, before retrying

	// OnChange is called with true when the volume disappears and false when
	// it is back. It runs on a goroutine of the Guard; keep it quick.
	OnChange func(down bool)

	mu      sync.Mutex
	down    bool
	up      chan struct{} // closed when the current outage ends
	outages int
}

// New returns a Guard for root when root lives on a mounted external volume
// (/Volumes/<name>/...), and nil otherwise: the internal disk does not go away.
func New(root string) *Guard {
	if !strings.HasPrefix(root, "/Volumes/") {
		return nil
	}
	return newGuard(root, func() bool { return mounted(root) }, time.Second, 2*time.Second)
}

func newGuard(root string, probe func() bool, poll, settle time.Duration) *Guard {
	return &Guard{root: root, probe: probe, poll: poll, settle: settle}
}

// Outages is how many times the volume disappeared.
func (g *Guard) Outages() int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.outages
}

// Root is the guarded path.
func (g *Guard) Root() string {
	if g == nil {
		return ""
	}
	return g.root
}

// Do runs op. If it fails and the volume has disappeared, Do waits for the
// volume to return and runs op again; it only gives up when ctx is cancelled.
// Errors listed in final, and errors on a healthy volume, are returned as they
// are: they are about the file, not the disk.
func (g *Guard) Do(ctx context.Context, op func() error, final ...error) error {
	retried := false
	for {
		err := op()
		if err == nil || g == nil || ctx.Err() != nil {
			return err
		}
		for _, f := range final {
			if errors.Is(err, f) {
				return err
			}
		}
		if !g.probe() {
			if werr := g.waitUp(ctx); werr != nil {
				return werr
			}
			retried = false
			continue
		}
		// The volume looks fine now. A drive that dropped and came back between
		// the failure and the check still deserves one more try.
		if looksLikeDisconnect(err) && !retried {
			retried = true
			if !sleep(ctx, g.settle) {
				return ctx.Err()
			}
			continue
		}
		return err
	}
}

func (g *Guard) waitUp(ctx context.Context) error {
	g.mu.Lock()
	if !g.down {
		g.down = true
		g.up = make(chan struct{})
		g.outages++
		go g.watch(ctx, g.up)
		if g.OnChange != nil {
			go g.OnChange(true)
		}
	}
	ch := g.up
	g.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// watch polls until the volume is back, then lets every waiter go.
func (g *Guard) watch(ctx context.Context, up chan struct{}) {
	for {
		if !sleep(ctx, g.poll) {
			return
		}
		if g.probe() {
			break
		}
	}
	if !sleep(ctx, g.settle) { // let the mount finish settling
		return
	}
	g.mu.Lock()
	g.down = false
	close(up)
	cb := g.OnChange
	g.mu.Unlock()
	if cb != nil {
		cb(false)
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// looksLikeDisconnect recognises the errors a vanishing disk produces,
// including the text ffmpeg prints, since subprocess failures are not errno values.
func looksLikeDisconnect(err error) bool {
	for _, e := range []error{syscall.ENXIO, syscall.EIO, syscall.ENODEV, syscall.ENOTCONN, syscall.ETIMEDOUT, syscall.ESTALE} {
		if errors.Is(err, e) {
			return true
		}
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "input/output error") || strings.Contains(s, "device not configured") ||
		strings.Contains(s, "no such device")
}

// NewWithProbe is New with a caller-supplied availability check. It exists so
// other packages can test how they behave across a disconnect.
func NewWithProbe(root string, probe func() bool, poll, settle time.Duration) *Guard {
	return newGuard(root, probe, poll, settle)
}
