// Package native owns a single authorized SDK process. It never exports keys.
package native

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"
)

type Config struct{ Helper, Runtime, Profile string }

// Session is reused across tracks. OpenKey and Decrypt must be called by one
// stream at a time. Close may interrupt that stream from another goroutine.
type Session struct {
	mu     sync.Mutex
	conn   nativeConnection
	rw     *bufio.ReadWriter
	abort  func()
	active bool
}

func Start(c Config) (*Session, error) {
	return StartContext(context.Background(), c)
}
func StartContext(ctx context.Context, c Config) (*Session, error) {
	ctx, cancel := context.WithCancel(ctx)
	conn, _, abort, err := startNative(ctx, c.Helper, c.Runtime, c.Profile)
	if err != nil {
		cancel()
		return nil, err
	}
	return &Session{conn: conn, rw: bufio.NewReadWriter(bufio.NewReaderSize(conn, 128<<10), bufio.NewWriterSize(conn, 128<<10)), abort: func() { cancel(); abort() }}, nil
}
func (s *Session) OpenKey(id, uri string) error {
	if len(id) == 0 || len(id) > 255 || len(uri) == 0 || len(uri) > 255 {
		return errors.New("invalid key request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return io.ErrClosedPipe
	}
	s.conn.SetDeadline(time.Now().Add(40 * time.Second))
	if s.active {
		if err := binary.Write(s.rw, binary.LittleEndian, uint32(0)); err != nil {
			return err
		}
	}
	s.rw.WriteByte(byte(len(id)))
	s.rw.WriteString(id)
	s.rw.WriteByte(byte(len(uri)))
	s.rw.WriteString(uri)
	if err := s.rw.Flush(); err != nil {
		return err
	}
	s.active = true
	return nil
}
func (s *Session) Decrypt(data []byte) error {
	n := len(data) &^ 15
	if n == 0 {
		return nil
	}
	if n > 4<<20 {
		return errors.New("sample too large")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil || !s.active {
		return io.ErrClosedPipe
	}
	s.conn.SetDeadline(time.Now().Add(40 * time.Second))
	if err := binary.Write(s.rw, binary.LittleEndian, uint32(n)); err != nil {
		return err
	}
	if _, err := s.rw.Write(data[:n]); err != nil {
		return err
	}
	if err := s.rw.Flush(); err != nil {
		return err
	}
	_, err := io.ReadFull(s.rw, data[:n])
	return err
}

// Abort stops only the process owned by this Session. It is safe during an RPC.
func (s *Session) Abort() { s.abort() }
func (s *Session) Close() { s.Abort(); s.mu.Lock(); s.conn = nil; s.mu.Unlock() }
