// Package runtimefiles installs checksum-pinned public runtime files separately
// from private account sessions. It never reads account material.
package runtimefiles

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed locks/*.json
var locks embed.FS

type item struct {
	Name string `json:"name"`
	Path string `json:"path"`
	SHA  string `json:"sha256"`
	URL  string `json:"url"`
}
type lock struct {
	Libraries    []item   `json:"libraries"`
	Sources      []string `json:"sources"`
	SourceCommit string   `json:"sourceCommit"`
	Loaders      []item   `json:"linuxLoaders"`
	Wrapper      struct {
		Source string `json:"source"`
		SHA    string `json:"sha256"`
	} `json:"wrapper"`
}

func Platform() (string, error) {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" || (runtime.GOOS == "windows" || runtime.GOOS == "linux") && runtime.GOARCH == "amd64" {
		return runtime.GOOS + "-" + runtime.GOARCH, nil
	}
	return "", errors.New("unsupported")
}
func download(ctx context.Context, url string) ([]byte, error) {
	r, e := http.NewRequestWithContext(ctx, "GET", url, nil)
	if e != nil {
		return nil, errors.New("setup")
	}
	c := http.Client{Timeout: 90 * time.Second}
	resp, e := c.Do(r)
	if e != nil {
		return nil, errors.New("setup")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("setup")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 80<<20+1))
	if e != nil || len(b) > 80<<20 {
		return nil, errors.New("setup")
	}
	return b, nil
}
func valid(b []byte, sha string) bool {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]) == sha
}
func save(path string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	tmp := path + ".part"
	if e := os.WriteFile(tmp, b, 0644); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
func install(ctx context.Context, path string, urls []string, sha string) error {
	if b, e := os.ReadFile(path); e == nil && valid(b, sha) {
		return nil
	}
	for _, u := range urls {
		if b, e := download(ctx, u); e == nil && valid(b, sha) {
			return save(path, b)
		}
	}
	return errors.New("setup")
}
func copyFile(source, target string) error {
	b, e := os.ReadFile(source)
	if e != nil {
		return errors.New("setup")
	}
	return save(target, b)
}

// Setup uses a native bundle built on the target OS. Apple SDK files are fetched
// separately from pinned upstream URLs and are never part of the source repo.
func Setup(ctx context.Context, dest, bundle string) error {
	key, e := Platform()
	if e != nil {
		return e
	}
	if _, e = os.Stat(filepath.Join(bundle, helperName())); e != nil {
		return errors.New("setup")
	}
	name := "x64.json"
	if strings.HasPrefix(key, "darwin") {
		name = "arm64.json"
	}
	raw, _ := locks.ReadFile("locks/" + name)
	var l lock
	if json.Unmarshal(raw, &l) != nil {
		return errors.New("setup")
	}
	libdir := dest
	if name == "x64.json" {
		libdir = filepath.Join(dest, "rootfs/system/lib64")
	}
	// Bounded concurrency keeps first setup short without retaining all SDK files.
	jobs := make(chan item)
	errs := make(chan error, 4)
	for n := 0; n < 4; n++ {
		go func() {
			var err error
			for v := range jobs {
				urls := []string{}
				if name == "arm64.json" {
					urls = []string{"https://raw.githubusercontent.com/WorldObservationLog/wrapper/" + l.SourceCommit + "/rootfs/system/lib64/" + v.Name}
				} else {
					for _, s := range l.Sources {
						urls = append(urls, s+"/"+v.Name)
					}
				}
				if err == nil {
					err = install(ctx, filepath.Join(libdir, v.Name), urls, v.SHA)
				}
			}
			errs <- err
		}()
	}
	for _, v := range l.Libraries {
		jobs <- v
	}
	close(jobs)
	var failure error
	for n := 0; n < 4; n++ {
		if e := <-errs; e != nil {
			failure = e
		}
	}
	if failure != nil {
		return failure
	}
	if name == "arm64.json" {
		target := filepath.Join(dest, "wrapper-main")
		if b, e := os.ReadFile(target); e == nil && valid(b, l.Wrapper.SHA) {
			return nil
		}
		b, e := download(ctx, l.Wrapper.Source)
		if e != nil {
			return e
		}
		z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if e != nil {
			return errors.New("setup")
		}
		for _, f := range z.File {
			if f.Name == "main" || strings.HasSuffix(f.Name, "/main") {
				r, e := f.Open()
				if e != nil {
					return e
				}
				b, e := io.ReadAll(io.LimitReader(r, 4<<20))
				r.Close()
				if e != nil || !valid(b, l.Wrapper.SHA) {
					return errors.New("setup")
				}
				return save(target, b)
			}
		}
		return errors.New("setup")
	}
	if runtime.GOOS == "linux" {
		for _, v := range l.Loaders {
			if e := install(ctx, filepath.Join(dest, v.Path), []string{v.URL}, v.SHA); e != nil {
				return e
			}
		}
	}
	// Bundle manifest pins all native host files, including the Windows GCC loader.
	var manifest map[string]string
	b, e := os.ReadFile(filepath.Join(bundle, "manifest.json"))
	if e != nil || json.Unmarshal(b, &manifest) != nil {
		return errors.New("setup")
	}
	for rel, sha := range manifest {
		if filepath.IsAbs(rel) || strings.Contains(rel, "..") || strings.Contains(rel, "\\") {
			return errors.New("setup")
		}
		b, e := os.ReadFile(filepath.Join(bundle, rel))
		if e != nil || !valid(b, sha) {
			return errors.New("setup")
		}
		if rel == helperName() {
			continue
		}
		if e = save(filepath.Join(dest, rel), b); e != nil {
			return e
		}
	}
	return nil
}
func helperName() string {
	if runtime.GOOS == "windows" {
		return "native-helper.exe"
	}
	return "native-helper"
}
func HelperName() string { return helperName() }
