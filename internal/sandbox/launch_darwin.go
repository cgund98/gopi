//go:build darwin

package sandbox

import (
	"context"
	"fmt"
)

// Launch runs argv under sandbox-exec, unless the profile is unsandboxed.
// The child is its own process group. A Seatbelt failure is returned as-is.
func Launch(ctx context.Context, profile Profile) (Result, error) {
	if profile.Name == ProfileUnsandboxed {
		return runChild(ctx, profile, profile.Argv)
	}
	if len(profile.Argv) == 0 {
		return Result{}, fmt.Errorf("command is empty")
	}
	policy, err := SeatbeltProfile(profile)
	if err != nil {
		return Result{}, err
	}
	argv := append([]string{"/usr/bin/sandbox-exec", "-p", policy}, profile.Argv...)
	return runChild(ctx, profile, argv)
}
