//go:build !darwin && !windows

package platform

import (
	"os"
	"time"
)

func BirthTime(os.FileInfo) time.Time { return time.Time{} }
