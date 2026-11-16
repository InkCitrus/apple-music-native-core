//go:build darwin && !mygo_portable_test

package native

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

func startNative(ctx context.Context, helper, runtime, profile string) (nativeConnection, func() error, func(), error) {
	pair, e := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if e != nil {
		return nil, nil, nil, e
	}
	parent := os.NewFile(uintptr(pair[0]), "native-audio-parent")
	child := os.NewFile(uintptr(pair[1]), "native-audio-child")
	conn, e := net.FileConn(parent)
	parent.Close()
	if e != nil {
		child.Close()
		return nil, nil, nil, e
	}
	cmd := exec.CommandContext(ctx, helper, runtime, "wrapper-main", "--decrypt-fd", profile, "3")
	cmd.ExtraFiles = []*os.File{child}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		child.Close()
		conn.Close()
		return nil, nil, nil, e
	}
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		child.Close()
		conn.Close()
		return nil, nil, nil, e
	}
	child.Close()
	var once sync.Once
	done := make(chan struct{})
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(done) }()
	abort := func() { once.Do(func() { conn.Close(); cmd.Process.Kill(); <-done }) }
	ready := make(chan bool, 1)
	go func() {
		sent := false
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			var event struct {
				Event         string `json:"event"`
				Authenticated bool   `json:"authenticated"`
			}
			if json.Unmarshal(scanner.Bytes(), &event) == nil && event.Event == "decryptPipeReady" {
				ready <- event.Authenticated
				sent = true
			}
		}
		if !sent {
			ready <- false
		}
	}()
	select {
	case ok := <-ready:
		if !ok {
			abort()
			return nil, nil, nil, errors.New("native playback session unavailable")
		}
	case <-ctx.Done():
		abort()
		return nil, nil, nil, ctx.Err()
	case <-time.After(40 * time.Second):
		abort()
		return nil, nil, nil, errors.New("native session start timed out")
	}
	return conn, func() error { <-done; return waitErr }, abort, nil
}
