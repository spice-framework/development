//go:build darwin

package libraryrelease

import "testing"

const expectedDarwinATFDCWD = ^uintptr(1)

// These two zero-length arrays are a compile-time equality assertion. The
// Darwin cross-build fails if darwinATFDCWD drifts in either direction, even
// when the test binary cannot execute on the build host.
var (
	_ [darwinATFDCWD - expectedDarwinATFDCWD]struct{}
	_ [expectedDarwinATFDCWD - darwinATFDCWD]struct{}
)

func TestDarwinATFDCWDContract(t *testing.T) {
	t.Parallel()
	value := uintptr(darwinATFDCWD)
	if got := int(value); got != -2 {
		t.Fatalf("Darwin AT_FDCWD = %d, want -2", got)
	}
}
