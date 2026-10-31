//go:build !darwin

package exact

import "os"

func setNoCache(*os.File) {}
