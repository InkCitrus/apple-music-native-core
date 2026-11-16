// amcore runs the private stdio daemon or opens system-native authorization.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/InkCitrus/apple-music-native-core/auth"
	"github.com/InkCitrus/apple-music-native-core/native"
	"github.com/InkCitrus/apple-music-native-core/runtimefiles"
	"github.com/InkCitrus/apple-music-native-core/service"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	base, _ := os.UserConfigDir()
	exe, _ := os.Executable()
	bin := filepath.Dir(exe)
	data := flag.String("data-dir", filepath.Join(base, "apple-music-native-core"), "private application data directory")
	bundle := flag.String("bundle", filepath.Join(bin, "native", runtime.GOOS+"-"+runtime.GOARCH), "native host bundle directory")
	runtimeDir := flag.String("runtime", "", "runtime directory (default: DATA/runtime)")
	helper := flag.String("helper", "", "native host executable (default: BUNDLE/native-helper)")
	session := flag.String("session", "", "dedicated session directory (default: DATA/session)")
	flag.Parse()
	ctx := context.Background()
	if _, e := runtimefiles.Platform(); e != nil {
		fmt.Fprintln(os.Stderr, "Unsupported platform/architecture")
		os.Exit(2)
	}
	if *runtimeDir == "" {
		*runtimeDir = filepath.Join(*data, "runtime")
	}
	if *helper == "" {
		*helper = filepath.Join(*bundle, runtimefiles.HelperName())
	}
	if *session == "" {
		*session = filepath.Join(*data, "session")
	}
	c := native.Config{Helper: *helper, Runtime: *runtimeDir, Profile: *session}
	setup := func(ctx context.Context) error { return runtimefiles.Setup(ctx, c.Runtime, *bundle) }
	mode := "serve"
	if len(flag.Args()) > 0 {
		mode = flag.Args()[0]
	}
	var err error
	switch mode {
	case "serve":
		err = service.New(service.Config{Native: c, Setup: setup}).Serve(ctx, os.Stdin, os.Stdout)
	case "setup":
		err = setup(ctx)
	case "auth":
		err = setup(ctx)
		if err == nil {
			err = auth.SignIn(ctx, c, auth.SystemPrompt{})
		}
	case "version":
		fmt.Println("apple-music-native-core 0.1.0 / protocol 1")
	default:
		err = fmt.Errorf("unknown command")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Native core operation failed; check the bundle, network and native sign-in.")
		os.Exit(1)
	}
}
