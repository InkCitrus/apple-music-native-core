// A minimal application embedding the Go player directly, without the daemon
// or MyGo Music. It logs playback state only, never account tokens or input.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/InkCitrus/apple-music-native-core/auth"
	"github.com/InkCitrus/apple-music-native-core/native"
	"github.com/InkCitrus/apple-music-native-core/player"
	"github.com/InkCitrus/apple-music-native-core/runtimefiles"
)

func main() {
	base, _ := os.UserConfigDir()
	exe, _ := os.Executable()
	data := flag.String("data-dir", filepath.Join(base, "apple-music-core-demo"), "dedicated demo data directory")
	bundle := flag.String("bundle", filepath.Join(filepath.Dir(exe), "native", runtime.GOOS+"-"+runtime.GOARCH), "native bundle supplied with the core")
	runtimeDir := flag.String("runtime", "", "runtime directory, default DATA/runtime")
	profile := flag.String("session", "", "dedicated native session, default DATA/session")
	track := flag.String("track", "", "catalog song ID to start playing")
	quality := flag.String("quality", "hires", "hires, lossless or aac")
	volume := flag.Float64("volume", 15, "initial volume, 0 to 100")
	language := flag.String("language", "en", "account window language: en or zh-CN")
	asJSON := flag.Bool("json", false, "emit safe machine-readable playback state")
	authenticate := flag.Bool("auth", false, "open the native account connection window before playing")
	flag.Parse()
	if _, err := runtimefiles.Platform(); err != nil {
		fmt.Fprintln(os.Stderr, "Unsupported platform")
		os.Exit(2)
	}
	if !player.SupportsQuality(*quality) || math.IsNaN(*volume) || math.IsInf(*volume, 0) || *volume < 0 || *volume > 100 {
		fmt.Fprintln(os.Stderr, "Invalid quality or volume")
		os.Exit(2)
	}
	if *runtimeDir == "" {
		*runtimeDir = filepath.Join(*data, "runtime")
	}
	if *profile == "" {
		*profile = filepath.Join(*data, "session")
	}
	cfg := native.Config{Helper: filepath.Join(*bundle, runtimefiles.HelperName()), Runtime: *runtimeDir, Profile: *profile}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	p := player.New(player.Config{Native: cfg})
	defer p.Close()
	_ = p.Volume(*volume)
	emit := func(event string, value any) {
		if *asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": event, "value": value})
		} else {
			fmt.Println(event, value)
		}
	}
	show := func() {
		s := p.State()
		if *asJSON {
			emit("state", s)
			return
		}
		format := "source pending"
		if s.Format != nil {
			format = fmt.Sprintf("ALAC %d-bit / %.1f kHz", s.Format.Bits, float64(s.Format.SampleRate)/1000)
		}
		fmt.Printf("%s  %d:%02d / %d:%02d  volume %.0f%%  %s  output %.1f kHz\n", s.Status, s.Position/60000, s.Position/1000%60, s.Duration/60000, s.Duration/1000%60, s.Volume, format, s.OutputRate/1000)
	}
	reportError := func(err error) {
		if err == nil {
			return
		}
		code := "unavailable"
		if err.Error() == "signIn" {
			code = "signIn"
		}
		emit("error", code)
	}
	signIn := func() error {
		p.Release()
		if err := runtimefiles.Setup(ctx, cfg.Runtime, *bundle); err != nil {
			return err
		}
		return auth.SignIn(ctx, cfg, auth.SystemPrompt{Language: *language, AppName: "Native Core Demo"})
	}
	if !*asJSON {
		fmt.Println("Native Core Demo — direct Go library embedding\nplay ID | pause | resume | seek SECONDS | volume 0..100 | state | stop | auth | quit")
	}
	if *authenticate {
		if err := signIn(); err != nil {
			reportError(err)
			return
		}
		emit("authorized", true)
	}
	if *track != "" {
		reportError(p.Play(ctx, *track, 0, *quality))
	}
	commands := make(chan string)
	go func() {
		defer close(commands)
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 1024), 4096)
		for scanner.Scan() {
			select {
			case commands <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	lastSnapshot := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snapshot := p.State()
			snapshot.Position /= 1000
			encoded, _ := json.Marshal(snapshot)
			if current := string(encoded); current != lastSnapshot {
				show()
				lastSnapshot = current
			}
		case line, ok := <-commands:
			if !ok {
				return
			}
			parts := strings.Fields(line)
			if len(parts) == 0 {
				continue
			}
			var err error
			number := func() (float64, bool) {
				if len(parts) != 2 {
					return 0, false
				}
				v, e := strconv.ParseFloat(parts[1], 64)
				return v, e == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
			}
			switch parts[0] {
			case "play":
				if len(parts) == 2 {
					err = p.Play(ctx, parts[1], 0, *quality)
				} else {
					emit("error", "play requires a catalog ID")
				}
			case "pause":
				err = p.Pause(true)
			case "resume":
				err = p.Pause(false)
			case "seek":
				if seconds, valid := number(); valid && seconds >= 0 && seconds <= math.MaxInt64/1000 {
					err = p.Seek(ctx, int64(seconds*1000), *quality)
				} else {
					emit("error", "invalid seek position")
				}
			case "volume":
				if percent, valid := number(); valid {
					err = p.Volume(percent)
				} else {
					emit("error", "invalid volume")
				}
			case "stop":
				p.Stop()
			case "state":
			case "auth":
				err = signIn()
				if err == nil {
					emit("authorized", true)
				}
			case "quit", "exit":
				return
			default:
				emit("error", "unknown command")
			}
			reportError(err)
			show()
		}
	}
}
