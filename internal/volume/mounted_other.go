//go:build !darwin

package volume

import (
	"os"
	"time"
)

// mounted on systems without macOS's /Volumes layout only checks that the
// path still answers, with a timeout because a dead mount can block.
func mounted(root string) bool {
	res := make(chan bool, 1)
	go func() {
		_, err := os.Stat(root)
		res <- err == nil
	}()
	select {
	case ok := <-res:
		return ok
	case <-time.After(3 * time.Second):
		return false
	}
}
