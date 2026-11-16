package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// The account window loads no SDK and makes no network requests. Its private pipe carries
// input only to this authentication flow; it never becomes a daemon response.
type windowEvent struct {
	Event    string `json:"event"`
	Username string `json:"username"`
	Password string `json:"password"`
	Code     string `json:"code"`
}
type windowPrompt struct {
	cmd     *exec.Cmd
	input   io.WriteCloser
	events  chan windowEvent
	done    chan struct{}
	mu      sync.Mutex
	once    sync.Once
	pending *windowEvent
}

func startWindow(ctx context.Context, cancel context.CancelFunc, binary string, settings SystemPrompt) (*windowPrompt, error) {
	cmd := exec.CommandContext(ctx, binary, "--pipe", "--language", settings.Language, "--app-name", settings.clientName())
	return startWindowCommand(ctx, cancel, cmd)
}

func startWindowCommand(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd) (*windowPrompt, error) {
	cmd.Stderr = io.Discard
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.New("account window unavailable")
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.New("account window unavailable")
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		return nil, errors.New("account window unavailable")
	}
	w := &windowPrompt{cmd: cmd, input: in, events: make(chan windowEvent, 1), done: make(chan struct{})}
	go func() {
		defer close(w.events)
		s := bufio.NewScanner(out)
		s.Buffer(make([]byte, 4096), 65536)
		for s.Scan() {
			var event windowEvent
			if json.Unmarshal(s.Bytes(), &event) != nil {
				continue
			}
			if event.Event == "cancel" {
				cancel()
				return
			}
			select {
			case w.events <- event:
			case <-ctx.Done():
				return
			}
		}
		cancel()
	}()
	go func() { cmd.Wait(); close(w.done) }()
	return w, nil
}
func (w *windowPrompt) send(view, stage string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return json.NewEncoder(w.input).Encode(map[string]string{"view": view, "stage": stage})
}
func (w *windowPrompt) receive(ctx context.Context, expected string) (windowEvent, error) {
	select {
	case event, ok := <-w.events:
		if !ok || event.Event != expected {
			return windowEvent{}, errors.New("sign-in cancelled")
		}
		return event, nil
	case <-ctx.Done():
		return windowEvent{}, errors.New("sign-in cancelled")
	}
}
func (w *windowPrompt) Credentials(ctx context.Context) (string, string, error) {
	if w.pending != nil {
		event := *w.pending
		w.pending = nil
		return strings.TrimSpace(event.Username), event.Password, nil
	}
	if err := w.send("credentials", ""); err != nil {
		return "", "", errors.New("account window unavailable")
	}
	event, err := w.receive(ctx, "credentials")
	return strings.TrimSpace(event.Username), event.Password, err
}
func (w *windowPrompt) Code(ctx context.Context) (string, error) {
	if err := w.send("verification", ""); err != nil {
		return "", errors.New("account window unavailable")
	}
	event, err := w.receive(ctx, "code")
	return event.Code, err
}
func (w *windowPrompt) Status(stage string) { w.send("progress", stage) }
func (w *windowPrompt) Retry(ctx context.Context, err error) bool {
	code := "unavailable"
	switch err.Error() {
	case "network", "account", "terms", "rejected":
		code = err.Error()
	}
	if w.send("failure", code) != nil {
		return false
	}
	event, e := w.receive(ctx, "retry")
	if e == nil {
		w.pending = &event
	}
	return e == nil
}
func (w *windowPrompt) Complete(ctx context.Context) {
	w.send("success", "")
	finished, cancel := context.WithTimeout(ctx, 1800*time.Millisecond)
	defer cancel()
	w.receive(finished, "done")
}
func (w *windowPrompt) Close() {
	w.once.Do(func() {
		w.input.Close()
		select {
		case <-w.done:
		case <-time.After(time.Second):
			w.cmd.Process.Kill()
			<-w.done
		}
	})
}
