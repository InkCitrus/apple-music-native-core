// Package auth forwards credentials from local system dialogs to the SDK over
// anonymous pipes. Passwords and verification codes are never command arguments.
package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/InkCitrus/apple-music-native-core/native"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Prompt may be replaced by an embedding application's native sign-in form.
type Prompt interface {
	Credentials(context.Context) (string, string, error)
	Code(context.Context) (string, error)
}
type SystemPrompt struct {
	Language string
	AppName  string
}

func output(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = io.Discard
	data, err := cmd.Output()
	if err != nil {
		return "", errors.New("sign-in cancelled or system dialog unavailable")
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"), nil
}
func (system SystemPrompt) Credentials(ctx context.Context) (string, string, error) {
	text := labels(system.Language)
	client := system.clientName()
	description := strings.ReplaceAll(text["description"], "%@", client)
	switch runtime.GOOS {
	case "darwin":
		return "", "", errors.New("use SignIn to open the native account window")
	case "windows":
		data, err := output(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-Command", system.windowsForm(false))
		if err != nil {
			return "", "", err
		}
		var fields []string
		if json.Unmarshal([]byte(data), &fields) != nil || len(fields) != 2 {
			return "", "", errors.New("invalid sign-in dialog")
		}
		return strings.TrimSpace(fields[0]), fields[1], nil
	default:
		if _, err := exec.LookPath("zenity"); err == nil {
			data, err := output(ctx, "zenity", "--forms", "--title="+client, "--text="+description, "--separator=\n", "--add-entry="+text["account_label"], "--add-password="+text["password_label"])
			fields := strings.SplitN(data, "\n", 2)
			if err != nil || len(fields) != 2 {
				return "", "", errors.New("sign-in cancelled")
			}
			return strings.TrimSpace(fields[0]), fields[1], nil
		}
		username, err := output(ctx, "kdialog", "--title", client, "--inputbox", text["account_label"])
		if err != nil {
			return "", "", err
		}
		password, err := output(ctx, "kdialog", "--title", client, "--password", text["password_label"])
		return strings.TrimSpace(username), password, err
	}
}
func (system SystemPrompt) Code(ctx context.Context) (string, error) {
	text := labels(system.Language)
	switch runtime.GOOS {
	case "darwin":
		return "", errors.New("use SignIn to open the native account window")
	case "windows":
		return output(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-Command", system.windowsForm(true))
	default:
		if _, err := exec.LookPath("zenity"); err == nil {
			return output(ctx, "zenity", "--entry", "--title="+system.clientName(), "--text="+text["verify_description"])
		}
		return output(ctx, "kdialog", "--title", system.clientName(), "--inputbox", text["verify_description"])
	}
}

// SignIn is interactive only through Prompt. Its result contains no credentials.
func SignIn(ctx context.Context, c native.Config, prompt Prompt) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	system, isSystem := prompt.(SystemPrompt)
	if pointer, ok := prompt.(*SystemPrompt); ok && pointer != nil {
		system, isSystem = *pointer, true
	}
	if isSystem && runtime.GOOS == "darwin" {
		window, err := startWindow(ctx, cancel, filepath.Join(filepath.Dir(c.Helper), "AccountWindow.app", "Contents", "MacOS", "account-ui"), system)
		if err != nil {
			return err
		}
		defer window.Close()
		for {
			err = signInOnce(ctx, c, window)
			if err == nil {
				window.Complete(ctx)
				return nil
			}
			if ctx.Err() != nil || !window.Retry(ctx, err) {
				return err
			}
		}
	}
	return signInOnce(ctx, c, prompt)
}

func signInOnce(ctx context.Context, c native.Config, prompt Prompt) error {
	username, password, err := prompt.Credentials(ctx)
	if err != nil {
		return err
	}
	if username == "" || password == "" {
		return errors.New("sign-in cancelled")
	}
	if status, ok := prompt.(interface{ Status(string) }); ok {
		status.Status("signingIn")
	}
	args := []string{"--runtime", c.Runtime, "--session", c.Profile, "--mode", "auth"}
	if runtime.GOOS == "darwin" {
		args = []string{c.Runtime, "wrapper-main", "--authenticate", c.Profile}
	}
	if err = os.MkdirAll(c.Profile, 0700); err != nil {
		return errors.New("private session directory unavailable")
	}
	cmd := exec.CommandContext(ctx, c.Helper, args...)
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return errors.New("native helper unavailable")
	}
	defer stdin.Close()
	defer cmd.Process.Kill()
	encoder := json.NewEncoder(stdin)
	err = encoder.Encode(map[string]string{"username": username, "password": password})
	username = ""
	password = ""
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return errors.New("authorization pipe closed")
	}
	accepted := false
	resultSeen := false
	failure := "rejected"
	s := bufio.NewScanner(stdout)
	s.Buffer(make([]byte, 4096), 65536)
	for s.Scan() {
		var e struct {
			Event     string `json:"event"`
			Challenge int    `json:"challenge"`
			Ready     bool   `json:"nativeSignInVerified"`
			Stage     string `json:"stage"`
			Kind      string `json:"kind"`
			Reason    string `json:"reason"`
		}
		if json.Unmarshal(s.Bytes(), &e) != nil {
			continue
		}
		if e.Event == "stage" && (e.Stage == "session_lease" || e.Stage == "account_tokens") {
			if status, ok := prompt.(interface{ Status(string) }); ok {
				status.Status("session")
			}
		}
		for _, reason := range []string{e.Kind, e.Reason} {
			switch reason {
			case "network_error":
				failure = "network"
			case "account_locked":
				failure = "account"
			case "terms_required":
				failure = "terms"
			}
		}
		if e.Event == "twoFactorRequired" {
			code, pe := prompt.Code(ctx)
			if pe != nil {
				cmd.Process.Kill()
				cmd.Wait()
				return pe
			}
			if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
				cmd.Process.Kill()
				cmd.Wait()
				return errors.New("invalid verification code")
			}
			writeErr := encoder.Encode(map[string]any{"method": "verify-code", "challenge": e.Challenge, "code": code})
			code = ""
			if writeErr != nil {
				cmd.Process.Kill()
				cmd.Wait()
				return errors.New("authorization pipe closed")
			}
			if status, ok := prompt.(interface{ Status(string) }); ok {
				status.Status("verifying")
			}
		}
		if e.Event == "authResult" {
			resultSeen = true
			accepted = e.Ready
		}
	}
	stdin.Close()
	err = cmd.Wait()
	if !resultSeen {
		return errors.New("unavailable")
	}
	if err != nil || !accepted {
		return errors.New(failure)
	}
	return nil
}
