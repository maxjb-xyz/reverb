package spotdl

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os/exec"
)

// scanLinesCR splits on EITHER '\n' or '\r'. spotDL/yt-dlp render progress by
// rewriting the same line with a carriage return (no newline), so the default
// bufio scanner would buffer those updates until a '\n' finally arrives — making
// live progress and output invisible. Splitting on '\r' too lets each update
// surface as it happens.
func scanLinesCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil // need more data
}

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
	sc.Split(scanLinesCR)
	for sc.Scan() {
		onLine(sc.Text())
	}
	werr := <-waitErr
	if werr != nil {
		return werr
	}
	return sc.Err()
}
