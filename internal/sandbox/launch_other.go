//go:build !darwin

package sandbox

import (
	"context"
	"fmt"
)

// Launch refuses to run a command without Seatbelt.
func Launch(context.Context, Profile) (Result, error) {
	return Result{}, fmt.Errorf("sandboxed shell is only available on macOS")
}
