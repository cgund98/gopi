//go:build !darwin

package sandbox

import (
	"context"
	"fmt"
)

// Launch runs an unsandboxed command directly. A sandboxed profile is refused.
func Launch(ctx context.Context, profile Profile) (Result, error) {
	if profile.Name == ProfileUnsandboxed {
		return runChild(ctx, profile, profile.Argv)
	}
	return Result{}, fmt.Errorf("sandboxed shell is only available on macOS")
}
