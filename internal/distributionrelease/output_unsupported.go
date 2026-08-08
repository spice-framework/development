//go:build !linux && !darwin && !windows

package distributionrelease

import (
	"fmt"
	"runtime"
)

func commitOutput(string, string) error {
	return fmt.Errorf("atomic no-replace distribution commit is unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
}
