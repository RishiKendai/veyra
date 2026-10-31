package volume

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// fakeDrive is a probe whose state the test controls.
type fakeDrive struct{ present atomic.Bool }

func (d *fakeDrive) probe() bool { return d.present.Load() }

func fast(d *fakeDrive) *Guard {
	return newGuard("/Volumes/X", d.probe, 5*time.Millisecond, 5*time.Millisecond)
}

func TestNilGuardIsAPassThrough(t *testing.T) {
	var g *Guard
	calls := 0
	err := g.Do(context.Background(), func() error { calls++; return errors.New("boom") })
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if g.Outages() != 0 || g.Root() != "" {
		t.Error("nil guard accessors")
	}
}

func TestSuccessAndGenuineErrorsAreReturnedUntouched(t *testing.T) {
	d := &fakeDrive{}
	d.present.Store(true)
	g := fast(d)
	if err := g.Do(context.Background(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	genuine := errors.New("corrupt image")
	calls := 0
	if err := g.Do(context.Background(), func() error { calls++; return genuine }); !errors.Is(err, genuine) || calls != 1 {
		t.Fatalf("a bad file on a healthy drive must not be retried: err=%v calls=%d", err, calls)
	}
	if g.Outages() != 0 {
		t.Error("no outage expected")
	}
}

func TestFinalErrorsSkipTheVolumeCheck(t *testing.T) {
	d := &fakeDrive{} // gone, but the error is about the file's content
	g := fast(d)
	notMedia := errors.New("not media")
	if err := g.Do(context.Background(), func() error { return fmt.Errorf("x: %w", notMedia) }, notMedia); !errors.Is(err, notMedia) {
		t.Fatal(err)
	}
	if g.Outages() != 0 {
		t.Error("a normal 'not a video' result must not look like an outage")
	}
}

func TestWaitsForTheDriveAndRetries(t *testing.T) {
	d := &fakeDrive{}
	g := fast(d)
	var changes []bool
	var mu sync.Mutex
	g.OnChange = func(down bool) { mu.Lock(); changes = append(changes, down); mu.Unlock() }

	attempts := 0
	done := make(chan error, 1)
	go func() {
		done <- g.Do(context.Background(), func() error {
			attempts++
			if !d.present.Load() {
				return syscall.ENXIO
			}
			return nil
		})
	}()
	time.Sleep(60 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("must keep waiting while the drive is gone, returned %v", err)
	default:
	}
	d.present.Store(true) // plugged back in
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("after the drive returned the retry should succeed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("never resumed")
	}
	if attempts != 2 || g.Outages() != 1 {
		t.Errorf("attempts=%d outages=%d", attempts, g.Outages())
	}
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(changes) != "[true false]" {
		t.Errorf("OnChange calls = %v, want [true false]", changes)
	}
}

func TestManyWorkersShareOneOutage(t *testing.T) {
	d := &fakeDrive{}
	g := fast(d)
	var wg sync.WaitGroup
	var failed atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := g.Do(context.Background(), func() error {
				if !d.present.Load() {
					return syscall.EIO
				}
				return nil
			})
			if err != nil {
				failed.Add(1)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	d.present.Store(true)
	wg.Wait()
	if failed.Load() != 0 {
		t.Errorf("%d workers gave up", failed.Load())
	}
	if g.Outages() != 1 {
		t.Errorf("one disconnect must count once, got %d", g.Outages())
	}
}

func TestCancelWhileWaiting(t *testing.T) {
	d := &fakeDrive{}
	g := fast(d)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.Do(ctx, func() error { return syscall.ENXIO }) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ctrl+C must stop the wait")
	}
}

func TestQuickReconnectGetsOneMoreTry(t *testing.T) {
	// The drive dropped and was back before the check: the error looks like a
	// disconnect, so the operation is retried once instead of being skipped.
	d := &fakeDrive{}
	d.present.Store(true)
	g := fast(d)
	calls := 0
	err := g.Do(context.Background(), func() error {
		calls++
		if calls == 1 {
			return fmt.Errorf("read: %w", syscall.EIO)
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	// But a persistent disconnect-looking error on a healthy volume is returned.
	calls = 0
	err = g.Do(context.Background(), func() error { calls++; return syscall.EIO })
	if !errors.Is(err, syscall.EIO) || calls != 2 {
		t.Fatalf("one retry only: err=%v calls=%d", err, calls)
	}
}

func TestLooksLikeDisconnect(t *testing.T) {
	yes := []error{syscall.ENXIO, syscall.EIO, fmt.Errorf("open x: %w", syscall.ENODEV),
		errors.New("ffmpeg: Input/output error"), errors.New("read: Device not configured")}
	no := []error{errors.New("corrupt image"), syscall.EACCES, syscall.ENOENT, errors.New("unknown duration")}
	for _, e := range yes {
		if !looksLikeDisconnect(e) {
			t.Errorf("%v should look like a disconnect", e)
		}
	}
	for _, e := range no {
		if looksLikeDisconnect(e) {
			t.Errorf("%v should not look like a disconnect", e)
		}
	}
}

func TestNewOnlyGuardsExternalVolumes(t *testing.T) {
	if New("/Users/someone/Pictures") != nil {
		t.Error("the internal disk needs no guard")
	}
	if g := New("/Volumes/HD1/Movies"); g == nil || g.Root() != "/Volumes/HD1/Movies" {
		t.Error("an external volume needs one")
	}
}

func TestMountedOnRealPaths(t *testing.T) {
	if mounted("/Users") {
		t.Error("/Users is on the boot volume, not under /Volumes")
	}
	if mounted("/Volumes/definitely-not-mounted-xyz") {
		t.Error("a missing mount point is not mounted")
	}
}
