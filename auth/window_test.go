package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWindowHelperProcess(t *testing.T) {
	mode := os.Getenv("AMCORE_TEST_ACCOUNT_WINDOW")
	if mode == "" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	credentialsRequests := 0
	for scanner.Scan() {
		var message map[string]string
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			os.Exit(3)
		}
		switch message["view"] {
		case "credentials":
			credentialsRequests++
			if credentialsRequests > 1 {
				os.Exit(4)
			}
			if mode == "unexpected" {
				encoder.Encode(windowEvent{Event: "unexpected"})
				continue
			}
			encoder.Encode(windowEvent{Event: "credentials", Username: "  synthetic@example.invalid  ", Password: " synthetic password "})
		case "progress":
			if mode == "cancel" {
				encoder.Encode(windowEvent{Event: "cancel"})
				os.Exit(0)
			}
		case "verification":
			encoder.Encode(windowEvent{Event: "code", Code: "123456"})
		case "failure":
			if strings.Contains(message["stage"], "PRIVATE") {
				os.Exit(5)
			}
			if mode != "no-retry" {
				encoder.Encode(windowEvent{Event: "retry", Username: "new@example.invalid", Password: " new password "})
			}
		case "success":
			encoder.Encode(windowEvent{Event: "done"})
		}
	}
	os.Exit(0)
}

func testWindow(t *testing.T, mode string) (*windowPrompt, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowHelperProcess$")
	cmd.Env = append(os.Environ(), "AMCORE_TEST_ACCOUNT_WINDOW="+mode)
	w, err := startWindowCommand(ctx, cancel, cmd)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close(); cancel() })
	return w, ctx
}

func TestNativeWindowCredentialsCodeAndManualRetry(t *testing.T) {
	w, ctx := testWindow(t, "normal")
	username, password, err := w.Credentials(ctx)
	if err != nil || username != "synthetic@example.invalid" || password != " synthetic password " {
		t.Fatal("credentials were not preserved")
	}
	if strings.Contains(strings.Join(w.cmd.Args, " "), "password") {
		t.Fatal("credentials entered process arguments")
	}
	w.Status("signingIn")
	code, err := w.Code(ctx)
	if err != nil || code != "123456" {
		t.Fatal("verification response missing")
	}
	if !w.Retry(ctx, errors.New("PRIVATE_NATIVE_DIAGNOSTIC")) {
		t.Fatal("manual retry failed")
	}
	username, password, err = w.Credentials(ctx)
	if err != nil || username != "new@example.invalid" || password != " new password " || w.pending != nil {
		t.Fatal("retry input was not consumed exactly once")
	}
	w.Complete(ctx)
}

func TestClosingWindowCancelsAuthentication(t *testing.T) {
	w, ctx := testWindow(t, "cancel")
	if _, _, err := w.Credentials(ctx); err != nil {
		t.Fatal(err)
	}
	w.Status("signingIn")
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("closing the UI did not cancel authentication")
	}
}

func TestRetryRequiresFreshUserInput(t *testing.T) {
	w, ctx := testWindow(t, "no-retry")
	if _, _, err := w.Credentials(ctx); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 80*time.Millisecond)
	defer cancel()
	if w.Retry(deadline, errors.New("rejected")) || w.pending != nil {
		t.Fatal("credentials were replayed without user input")
	}
}

func TestUnexpectedWindowReplyIsRejected(t *testing.T) {
	w, ctx := testWindow(t, "unexpected")
	username, password, err := w.Credentials(ctx)
	if err == nil || username != "" || password != "" {
		t.Fatal("unexpected reply accepted")
	}
}
