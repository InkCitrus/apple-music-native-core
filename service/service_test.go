package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/InkCitrus/apple-music-native-core/native"
	"strings"
	"testing"
)

type cancelledPrompt struct{}

func (cancelledPrompt) Credentials(context.Context) (string, string, error) {
	return "", "", errors.New("SENSITIVE_ERROR_MUST_NOT_LEAVE_PROMPT")
}
func (cancelledPrompt) Code(context.Context) (string, error) { panic("unexpected code prompt") }
func TestJSONLContractAndSanitizedFailure(t *testing.T) {
	s := New(Config{Native: native.Config{Profile: t.TempDir()}, Prompt: cancelledPrompt{}})
	input := `{"id":1,"method":"hello"}
{"id":2,"method":"volume","params":{"percent":0}}
{"id":3,"method":"state"}
{"id":4,"method":"auth"}
{"id":5,"method":"no-such-method"}
`
	var out bytes.Buffer
	if e := s.Serve(context.Background(), strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "SENSITIVE") {
		t.Fatal("prompt error leaked")
	}
	seen := map[int64]response{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var r response
		if e := json.Unmarshal([]byte(line), &r); e != nil {
			t.Fatal(e)
		}
		seen[r.ID] = r
	}
	if len(seen) != 5 || !seen[1].OK || !seen[2].OK || seen[4].Error.Code != "signIn" || seen[5].Error.Code != "unsupported" {
		t.Fatal(out.String())
	}
}
func TestLogoutRefusesRoot(t *testing.T) {
	s := New(Config{Native: native.Config{Profile: "/"}})
	defer s.player.Close()
	if _, e := s.Call(context.Background(), Request{Method: "logout"}); e == nil {
		t.Fatal("root profile accepted")
	}
}
