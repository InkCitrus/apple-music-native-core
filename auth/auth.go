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
	"runtime"
	"strings"
	"time"
)

// Prompt may be replaced by an embedding application's native sign-in form.
type Prompt interface {
	Credentials(context.Context) (string, string, error)
	Code(context.Context) (string, error)
}
type SystemPrompt struct{}

func output(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = io.Discard
	data, err := cmd.Output()
	if err != nil {
		return "", errors.New("sign-in cancelled or system dialog unavailable")
	}
	return strings.TrimSpace(string(data)), nil
}
func (SystemPrompt) Credentials(ctx context.Context) (string, string, error) {
	switch runtime.GOOS {
	case "darwin":
		u, e := output(ctx, "/usr/bin/osascript", "-e", `text returned of (display dialog "Apple Music: Apple Account" default answer "" with title "Apple Music Native Core")`)
		if e != nil {
			return "", "", e
		}
		p, e := output(ctx, "/usr/bin/osascript", "-e", `text returned of (display dialog "Apple Music: Password" default answer "" with hidden answer with title "Apple Music Native Core")`)
		return u, p, e
	case "windows":
		s, e := output(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-Command", windowsForm(false))
		if e != nil {
			return "", "", e
		}
		var fields []string
		if json.Unmarshal([]byte(s), &fields) != nil || len(fields) != 2 {
			return "", "", errors.New("invalid sign-in dialog")
		}
		return fields[0], fields[1], nil
	default:
		if _, e := exec.LookPath("zenity"); e == nil {
			s, e := output(ctx, "zenity", "--forms", "--title=Apple Music Native Core", "--text=Sign in to Apple Music", "--separator=\n", "--add-entry=Apple Account", "--add-password=Password")
			a := strings.SplitN(s, "\n", 2)
			if e != nil || len(a) != 2 {
				return "", "", errors.New("sign-in cancelled")
			}
			return a[0], a[1], nil
		}
		u, e := output(ctx, "kdialog", "--title", "Apple Music Native Core", "--inputbox", "Apple Account")
		if e != nil {
			return "", "", e
		}
		p, e := output(ctx, "kdialog", "--title", "Apple Music Native Core", "--password", "Password")
		return u, p, e
	}
}
func (SystemPrompt) Code(ctx context.Context) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return output(ctx, "/usr/bin/osascript", "-e", `text returned of (display dialog "Apple Music: 6-digit verification code" default answer "" with title "Apple Music Native Core")`)
	case "windows":
		return output(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-Command", windowsForm(true))
	default:
		if _, e := exec.LookPath("zenity"); e == nil {
			return output(ctx, "zenity", "--entry", "--title=Apple Music Native Core", "--text=6-digit verification code")
		}
		return output(ctx, "kdialog", "--title", "Apple Music Native Core", "--inputbox", "6-digit verification code")
	}
}
func windowsForm(code bool) string {
	// All script text is static. Only the anonymous stdout pipe returns user input.
	fields := `$u=New-Object Windows.Forms.TextBox;$u.Location=New-Object Drawing.Point(20,45);$u.Width=320;$f.Controls.Add($u);$p=New-Object Windows.Forms.TextBox;$p.Location=New-Object Drawing.Point(20,100);$p.Width=320;$p.UseSystemPasswordChar=$true;$f.Controls.Add($p);$l.Text='Apple Account and password';`
	result := `ConvertTo-Json -Compress -InputObject @($u.Text,$p.Text)`
	if code {
		fields = `$u=New-Object Windows.Forms.TextBox;$u.Location=New-Object Drawing.Point(20,65);$u.Width=320;$u.MaxLength=6;$f.Controls.Add($u);$l.Text='6-digit verification code';`
		result = `[Console]::WriteLine($u.Text)`
	}
	return `[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false);Add-Type -AssemblyName System.Windows.Forms;Add-Type -AssemblyName System.Drawing;$f=New-Object Windows.Forms.Form;$f.Text='Apple Music Native Core';$f.ClientSize=New-Object Drawing.Size(365,190);$f.StartPosition='CenterScreen';$f.TopMost=$true;$l=New-Object Windows.Forms.Label;$l.Location=New-Object Drawing.Point(20,15);$l.Width=325;$f.Controls.Add($l);` + fields + `$b=New-Object Windows.Forms.Button;$b.Text='Continue';$b.Location=New-Object Drawing.Point(240,145);$b.DialogResult=[Windows.Forms.DialogResult]::OK;$f.Controls.Add($b);$f.AcceptButton=$b;if($f.ShowDialog() -ne [Windows.Forms.DialogResult]::OK){exit 1};` + result
}

// SignIn is interactive only through Prompt. Its result contains no credentials.
func SignIn(ctx context.Context, c native.Config, prompt Prompt) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	username, password, err := prompt.Credentials(ctx)
	if err != nil {
		return err
	}
	if username == "" || password == "" {
		return errors.New("sign-in cancelled")
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
		cmd.Wait()
		return errors.New("authorization pipe closed")
	}
	accepted := false
	s := bufio.NewScanner(stdout)
	s.Buffer(make([]byte, 4096), 65536)
	for s.Scan() {
		var e struct {
			Event     string `json:"event"`
			Challenge int    `json:"challenge"`
			Ready     bool   `json:"nativeSignInVerified"`
		}
		if json.Unmarshal(s.Bytes(), &e) != nil {
			continue
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
			encoder.Encode(map[string]any{"method": "verify-code", "challenge": e.Challenge, "code": code})
			code = ""
		}
		if e.Event == "authResult" {
			accepted = e.Ready
		}
	}
	stdin.Close()
	err = cmd.Wait()
	if err != nil || !accepted {
		return errors.New("native authorization failed")
	}
	return nil
}
