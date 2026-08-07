//go:build !linux && !darwin && !windows

package gorelease

import (
	"fmt"
	"runtime"
)

func commitOutput(string, string) error {
	return fmt.Errorf("atomic no-replace Go release commit is unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
}
