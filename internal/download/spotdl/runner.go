package spotdl

import (
	"bufio"
	"context"
	"os/exec"
)

// Runner streams a process's combined stdout/stderr line-by-line. Abstracted so
// the parser is unit-testable with canned output and no real downloads occur.
type Runner interface {
	Run(ctx context.Context, name string, args []string, onLine func(string)) error
}

// ExecRunner is the production Runner. It uses os/exec with a piped stdout so
// progress lines stream as spotDL emits them. The ctx is honored: canceling it
// kills the child process (exec.CommandContext).
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // spotDL writes progress to stdout; merge any stderr too
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		onLine(sc.Text())
	}
	return cmd.Wait()
}
