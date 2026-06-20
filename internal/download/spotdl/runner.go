package spotdl

import (
	"bufio"
	"context"
	"io"
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

func (r ExecRunner) Run(ctx context.Context, name string, args []string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, name, args...)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		return err
	}
	waitErr := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = pw.CloseWithError(err) // unblocks the scanner with EOF (or the err)
		waitErr <- err
	}()
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		onLine(sc.Text())
	}
	werr := <-waitErr
	if werr != nil {
		return werr
	}
	return sc.Err()
}
