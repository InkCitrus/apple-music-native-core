//go:build linux || windows || mygo_portable_test

package native

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

type nativeReply struct {
	ID            int    `json:"id"`
	OK            bool   `json:"ok"`
	Data          string `json:"data"`
	Event         string `json:"event"`
	Authenticated bool   `json:"authenticated"`
	Stage         string `json:"stage"`
	Reason        string `json:"reason"`
	Code          int    `json:"code"`
}
type rpcPipe struct {
	input          io.WriteCloser
	reply          <-chan nativeReply
	pending        []byte
	output         bytes.Buffer
	active, closed bool
	id             int
	deadline       time.Time
}

func (p *rpcPipe) call(method string, fields map[string]any) (nativeReply, error) {
	p.id++
	fields["id"] = p.id
	fields["method"] = method
	if e := json.NewEncoder(p.input).Encode(fields); e != nil {
		return nativeReply{}, errors.New("native command pipe closed")
	}
	timeout := 40 * time.Second
	if !p.deadline.IsZero() {
		timeout = time.Until(p.deadline)
		if timeout <= 0 {
			return nativeReply{}, errors.New("native command deadline exceeded")
		}
	}
	select {
	case reply, ok := <-p.reply:
		if !ok {
			return reply, errors.New("native helper terminated")
		}
		if reply.ID != p.id || !reply.OK {
			return reply, fmt.Errorf("native %s failed (code %d)", method, reply.Code)
		}
		return reply, nil
	case <-time.After(timeout):
		return nativeReply{}, errors.New("native operation timed out")
	}
}
func (p *rpcPipe) Write(data []byte) (int, error) {
	if p.closed {
		return 0, io.ErrClosedPipe
	}
	p.pending = append(p.pending, data...)
	for {
		if !p.active {
			if len(p.pending) < 1 {
				break
			}
			n := int(p.pending[0])
			if n == 0 {
				p.pending = p.pending[1:]
				_, e := p.call("close", map[string]any{})
				p.closed = true
				return len(data), e
			}
			if len(p.pending) < n+2 {
				break
			}
			m := int(p.pending[n+1])
			if len(p.pending) < n+m+2 {
				break
			}
			adam := string(p.pending[1 : n+1])
			uri := string(p.pending[n+2 : n+m+2])
			p.pending = p.pending[n+m+2:]
			if _, e := p.call("openKey", map[string]any{"trackID": adam, "uri": uri}); e != nil {
				return 0, e
			}
			p.active = true
			continue
		}
		if len(p.pending) < 4 {
			break
		}
		n := int(binary.LittleEndian.Uint32(p.pending))
		if n > 4<<20 {
			return 0, errors.New("native sample size limit exceeded")
		}
		if n == 0 {
			p.pending = p.pending[4:]
			p.active = false
			continue
		}
		if len(p.pending) < n+4 {
			break
		}
		payload := base64.StdEncoding.EncodeToString(p.pending[4 : n+4])
		p.pending = p.pending[n+4:]
		reply, e := p.call("decrypt", map[string]any{"data": payload})
		if e != nil {
			return 0, e
		}
		clear, e := base64.StdEncoding.DecodeString(reply.Data)
		if e != nil || len(clear) != n {
			return 0, errors.New("invalid native sample response")
		}
		p.output.Write(clear)
	}
	return len(data), nil
}
func (p *rpcPipe) Read(data []byte) (int, error) {
	if p.output.Len() == 0 {
		return 0, errors.New("native sample response missing")
	}
	return p.output.Read(data)
}
func (p *rpcPipe) Close() error                  { p.closed = true; return p.input.Close() }
func (p *rpcPipe) SetDeadline(t time.Time) error { p.deadline = t; return nil }
func startNative(ctx context.Context, helper, runtime, profile string) (nativeConnection, func() error, func(), error) {
	cmd := exec.CommandContext(ctx, helper, "--runtime", runtime, "--session", profile, "--mode", "serve")
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return nil, nil, nil, e
	}
	stdin, e := cmd.StdinPipe()
	if e != nil {
		return nil, nil, nil, e
	}
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		return nil, nil, nil, e
	}
	replies := make(chan nativeReply, 1)
	ready := make(chan nativeReply, 1)
	go func() {
		defer close(replies)
		sent := false
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 8<<20)
		for scanner.Scan() {
			var r nativeReply
			if json.Unmarshal(scanner.Bytes(), &r) != nil {
				continue
			}
			if r.Event == "decryptPipeReady" || r.Event == "failure" {
				if !sent {
					ready <- r
					sent = true
				}
				continue
			}
			if r.ID > 0 {
				replies <- r
			}
		}
		if !sent {
			ready <- nativeReply{Reason: "native_helper_terminated"}
		}
	}()
	var once sync.Once
	done := make(chan struct{})
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(done) }()
	abort := func() { once.Do(func() { stdin.Close(); cmd.Process.Kill(); <-done }) }
	select {
	case r := <-ready:
		if !r.Authenticated {
			abort()
			return nil, nil, nil, fmt.Errorf("native initialization failed: %s/%s", r.Stage, r.Reason)
		}
	case <-ctx.Done():
		abort()
		return nil, nil, nil, ctx.Err()
	case <-time.After(40 * time.Second):
		abort()
		return nil, nil, nil, errors.New("native initialization timed out")
	}
	wait := func() error {
		select {
		case <-done:
			return waitErr
		case <-time.After(15 * time.Second):
			abort()
			return errors.New("native shutdown timed out")
		}
	}
	return &rpcPipe{input: stdin, reply: replies}, wait, abort, nil
}
