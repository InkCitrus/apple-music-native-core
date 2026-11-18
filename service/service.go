// Package service exposes a versioned JSON Lines interface over private stdio.
package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/InkCitrus/apple-music-native-core/auth"
	"github.com/InkCitrus/apple-music-native-core/catalog"
	"github.com/InkCitrus/apple-music-native-core/native"
	"github.com/InkCitrus/apple-music-native-core/player"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const ProtocolVersion = 1
const Version = "0.1.4"

type Config struct {
	Native native.Config
	Setup  func(context.Context) error
	Prompt auth.Prompt
}
type Request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
type response struct {
	ID     int64  `json:"id"`
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  *fault `json:"error,omitempty"`
}
type fault struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Server struct {
	config         Config
	player         *player.Player
	writer         io.Writer
	writeMu        sync.Mutex
	authMu         sync.Mutex
	authenticating bool
	quality        string
	mu             sync.Mutex
}

func New(c Config) *Server {
	if c.Prompt == nil {
		c.Prompt = auth.SystemPrompt{}
	}
	return &Server{config: c, player: player.New(player.Config{Native: c.Native}), quality: "hires"}
}
func (s *Server) emit(r response) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	json.NewEncoder(s.writer).Encode(r)
}

// Serve closes all owned processes when stdin closes. It listens on no sockets.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	s.writer = out
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer s.player.Close()
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 65536)
	var jobs sync.WaitGroup
	for scanner.Scan() {
		var r Request
		if json.Unmarshal(scanner.Bytes(), &r) != nil || r.ID <= 0 {
			s.emit(response{ID: r.ID, OK: false, Error: &fault{"invalid", "Invalid request"}})
			continue
		}
		if r.Method == "close" {
			cancel()
			s.emit(response{ID: r.ID, OK: true, Result: true})
			break
		}
		// State and transport controls stay responsive while native authorization is open.
		jobs.Add(1)
		go func(r Request) {
			defer jobs.Done()
			v, e := s.Call(ctx, r)
			reply := response{ID: r.ID, OK: e == nil, Result: v}
			if e != nil {
				code := "unavailable"
				switch e.Error() {
				case "signIn", "unsupported", "setup", "busy", "invalid":
					code = e.Error()
				}
				reply.Result = nil
				reply.Error = &fault{code, message(code)}
			}
			s.emit(reply)
		}(r)
	}
	cancel()
	jobs.Wait()
	return scanner.Err()
}
func message(code string) string {
	switch code {
	case "signIn":
		return "Sign in using the native account window"
	case "setup":
		return "Install or repair the native runtime"
	case "unsupported":
		return "This track, quality or platform is not supported"
	case "busy":
		return "Native authorization is already in progress"
	case "invalid":
		return "Invalid request"
	}
	return "Native operation failed"
}
func (s *Server) Call(ctx context.Context, r Request) (any, error) {
	var p struct {
		TrackID     string   `json:"trackId"`
		NextTrackID string   `json:"nextTrackId"`
		Position    int64    `json:"position"`
		Percent     *float64 `json:"percent"`
		Quality     string   `json:"quality"`
	}
	if len(r.Params) > 0 && json.Unmarshal(r.Params, &p) != nil {
		return nil, errors.New("invalid")
	}
	switch r.Method {
	case "hello":
		return map[string]any{"protocol": ProtocolVersion, "version": Version, "formats": []string{"alac"}, "devices": []string{"default"}}, nil
	case "doctor":
		_, he := os.Stat(s.config.Native.Helper)
		_, te := catalog.LoadTokens(s.config.Native.Profile)
		return map[string]any{"helperPresent": he == nil, "sessionPresent": te == nil}, nil
	case "state":
		return s.player.State(), nil
	case "devices":
		return []map[string]any{{"id": "default", "name": "System Audio Output", "active": true}}, nil
	case "play", "seek":
		s.authMu.Lock()
		defer s.authMu.Unlock()
		if s.authenticating {
			return nil, errors.New("busy")
		}
		s.mu.Lock()
		q := s.quality
		s.mu.Unlock()
		if p.Quality != "" {
			q = p.Quality
		}
		var e error
		if r.Method == "play" {
			e = s.player.PlayWithNext(ctx, p.TrackID, p.Position, q, p.NextTrackID)
		} else {
			e = s.player.Seek(ctx, p.Position, q)
		}
		return s.player.State(), e
	case "pause":
		return true, s.player.Pause(true)
	case "resume":
		return true, s.player.Pause(false)
	case "volume":
		if p.Percent == nil {
			return nil, errors.New("invalid")
		}
		return true, s.player.Volume(*p.Percent)
	case "stop":
		s.player.Stop()
		return true, nil
	case "quality":
		if p.Quality != "hires" && p.Quality != "lossless" {
			return nil, errors.New("unsupported")
		}
		s.mu.Lock()
		s.quality = p.Quality
		s.mu.Unlock()
		return true, nil
	case "setup":
		s.authMu.Lock()
		defer s.authMu.Unlock()
		if s.authenticating {
			return nil, errors.New("busy")
		}
		if s.config.Setup == nil {
			return nil, errors.New("setup")
		}
		return true, s.config.Setup(ctx)
	case "auth":
		s.authMu.Lock()
		if s.authenticating {
			s.authMu.Unlock()
			return nil, errors.New("busy")
		}
		s.authenticating = true
		s.authMu.Unlock()
		defer func() { s.authMu.Lock(); s.authenticating = false; s.authMu.Unlock() }()
		s.player.Release()
		if s.config.Setup != nil {
			if e := s.config.Setup(ctx); e != nil {
				return nil, errors.New("setup")
			}
		}
		if e := auth.SignIn(ctx, s.config.Native, s.config.Prompt); e != nil {
			return nil, errors.New("signIn")
		}
		return true, nil
	case "session":
		return catalog.LoadTokens(s.config.Native.Profile)
	case "logout":
		s.authMu.Lock()
		defer s.authMu.Unlock()
		if s.authenticating {
			return nil, errors.New("busy")
		}
		s.player.Release()
		// Profile is a dedicated directory selected by the embedding application.
		if s.config.Native.Profile == "" || filepath.Clean(s.config.Native.Profile) == "." || filepath.Dir(filepath.Clean(s.config.Native.Profile)) == filepath.Clean(s.config.Native.Profile) {
			return nil, errors.New("invalid")
		}
		return true, os.RemoveAll(s.config.Native.Profile)
	default:
		return nil, errors.New("unsupported")
	}
}
