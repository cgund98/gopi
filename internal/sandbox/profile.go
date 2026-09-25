package sandbox

import "time"

const (
	// NetworkDeny blocks every outbound socket, including DNS.
	NetworkDeny = "deny"

	DefaultTimeout     = 30 * time.Second
	DefaultOutputLimit = 64 * 1024
	ProfileSandbox     = "sandbox"
)

// Profile is the host-computed sandbox for one command.
// The child cannot widen it.
type Profile struct {
	Name        string
	Home        string
	ReadRoots   []string
	WriteRoots  []string
	DenyRead    []string
	DenyWrite   []string
	Network     string
	Env         []string
	Timeout     time.Duration
	OutputLimit int
	WorkDir     string
	Argv        []string
}

// Result is the capped output of one sandboxed command.
type Result struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Truncated bool
}
