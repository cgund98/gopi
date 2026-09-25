package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"
)

func runChild(ctx context.Context, profile Profile, argv []string) (Result, error) {
	if len(argv) == 0 {
		return Result{}, fmt.Errorf("command is empty")
	}
	timeout := profile.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	limit := profile.OutputLimit
	if limit <= 0 {
		limit = DefaultOutputLimit
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = profile.WorkDir
	cmd.Env = profile.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start command: %w", err)
	}

	type captured struct {
		text  string
		trunc bool
	}
	outCh := make(chan captured, 1)
	errCh := make(chan captured, 1)
	go func() {
		text, trunc := readLimited(stdout, limit)
		outCh <- captured{text, trunc}
	}()
	go func() {
		text, trunc := readLimited(stderr, limit)
		errCh <- captured{text, trunc}
	}()
	out := <-outCh
	errOut := <-errCh
	waitErr := cmd.Wait()
	result := Result{
		Stdout:    out.text,
		Stderr:    errOut.text,
		Truncated: out.trunc || errOut.trunc,
		ExitCode:  exitCode(waitErr),
	}
	if runCtx.Err() != nil && ctx.Err() == nil {
		return result, fmt.Errorf("command timed out")
	}
	if ctx.Err() != nil {
		return result, fmt.Errorf("command cancelled")
	}
	return result, nil
}

func readLimited(r io.Reader, limit int) (string, bool) {
	buf := make([]byte, limit+1)
	n, _ := io.ReadFull(r, buf)
	truncated := n > limit
	if n > limit {
		n = limit
	}
	_, _ = io.Copy(io.Discard, r)
	return string(buf[:n]), truncated
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
