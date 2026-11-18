package stream

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/InkCitrus/apple-music-native-core/catalog"
	"github.com/InkCitrus/apple-music-native-core/codec"
)

// Apple Music's six live stations are also distributed by TuneIn. Resolve
// the public broadcast at play time, without sharing catalog credentials.
var tuneInStations = map[string]string{
	"ra.978194965":  "s345732", // Apple Music 1
	"ra.1498155548": "s345724", // Hits
	"ra.1498157166": "s345725", // Country
	"ra.1740613859": "s345726", // Club
	"ra.1740613864": "s345727", // Música Uno
	"ra.1740614260": "s345733", // Chill
}

const (
	tuneInHost      = "opml.radiotime.com"
	radioListLimit  = 512 << 10
	radioAudioLimit = 2 << 20
	radioTimeout    = 15 * time.Second
)

func radioURLAllowed(u *url.URL, directory bool) bool {
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	if directory {
		return u.Hostname() == tuneInHost
	}
	switch u.Hostname() {
	case "itsliveradio.apple.com", "itsliveradiobackup.apple.com",
		"liveradio.music.apple.com", "liveradiobackup.music.apple.com":
		return true
	}
	return false
}

var radioHTTP = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(req *http.Request, from []*http.Request) error {
	if len(from) >= 5 || !radioURLAllowed(req.URL, from[0].URL.Hostname() == tuneInHost) {
		return errors.New("invalid broadcast redirect")
	}
	return nil
}}

func getRadio(ctx context.Context, client *http.Client, u *url.URL, limit int64, directory bool) ([]byte, *url.URL, error) {
	if !radioURLAllowed(u, directory) {
		return nil, nil, errors.New("invalid broadcast URL")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, nil, errors.New("invalid broadcast request")
	}
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, nil, errors.New("broadcast request timed out")
		}
		return nil, nil, errors.New("broadcast connection failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("broadcast HTTP status %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || len(b) == 0 || int64(len(b)) > limit {
		return nil, nil, errors.New("invalid broadcast response")
	}
	return b, res.Request.URL, nil
}

type radioRendition struct {
	url       *url.URL
	bandwidth int
}

func playlistLines(b []byte) ([]string, error) {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "#EXTM3U" {
		return nil, errors.New("invalid broadcast playlist")
	}
	return lines[1:], nil
}

func radioRenditions(b []byte, base *url.URL) ([]radioRendition, error) {
	lines, err := playlistLines(b)
	if err != nil {
		return nil, err
	}
	var result []radioRendition
	for i, line := range lines[:len(lines)-1] {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-SESSION-KEY:") {
			return nil, errors.New("encrypted broadcast format")
		}
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF:") || catalog.Attribute(line, "CODECS") != "mp4a.40.2" {
			continue
		}
		target, err := base.Parse(strings.TrimSpace(lines[i+1]))
		if err != nil || !radioURLAllowed(target, false) || target.Fragment != "" {
			continue
		}
		bandwidth, _ := strconv.Atoi(catalog.Attribute(line, "BANDWIDTH"))
		result = append(result, radioRendition{target, bandwidth})
	}
	slices.SortStableFunc(result, func(a, b radioRendition) int { return cmp.Compare(b.bandwidth, a.bandwidth) })
	if len(result) == 0 {
		return nil, errors.New("no AAC-LC broadcast rendition")
	}
	return result, nil
}

type radioSegment struct {
	url           *url.URL
	discontinuity bool
}

type radioPlaylist struct {
	segs   []radioSegment
	first  int64
	target time.Duration
}

func parseRadioPlaylist(b []byte, base *url.URL) (radioPlaylist, error) {
	var out radioPlaylist
	lines, err := playlistLines(b)
	if err != nil {
		return out, err
	}
	hasSequence := false
	pendingSegment, discontinuity := false, false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			if catalog.Attribute(line, "METHOD") != "NONE" {
				return out, errors.New("encrypted broadcast format")
			}
		case line == "#EXT-X-ENDLIST", strings.HasPrefix(line, "#EXT-X-SESSION-KEY:"),
			strings.HasPrefix(line, "#EXT-X-MAP:"), strings.HasPrefix(line, "#EXT-X-BYTERANGE:"),
			strings.HasPrefix(line, "#EXT-X-SKIP:"), line == "#EXT-X-GAP":
			return out, errors.New("unsupported broadcast playlist")
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			out.first, err = strconv.ParseInt(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"), 10, 56)
			if err != nil || out.first < 0 {
				return out, errors.New("invalid broadcast sequence")
			}
			hasSequence = true
		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			n, err := strconv.Atoi(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"))
			if err != nil || n < 1 || n > 60 {
				return out, errors.New("invalid broadcast duration")
			}
			out.target = time.Duration(n) * time.Second
		case line == "#EXT-X-DISCONTINUITY":
			discontinuity = true
		case strings.HasPrefix(line, "#EXTINF:"):
			pendingSegment = true
		case line != "" && !strings.HasPrefix(line, "#"):
			target, err := base.Parse(line)
			if err != nil || !pendingSegment || !radioURLAllowed(target, false) || len(out.segs) >= 256 {
				return out, errors.New("invalid broadcast segment")
			}
			out.segs = append(out.segs, radioSegment{target, discontinuity})
			pendingSegment, discontinuity = false, false
		}
	}
	if !hasSequence || out.target == 0 || len(out.segs) == 0 {
		return out, errors.New("empty broadcast window")
	}
	return out, nil
}

func (p radioPlaylist) start() int64 {
	// Leave one completed segment ahead for the initial network buffer.
	return p.first + int64(max(0, len(p.segs)-2))
}

func readRadioPlaylist(ctx context.Context, client *http.Client, u *url.URL) (radioPlaylist, error) {
	b, base, err := getRadio(ctx, client, u, radioListLimit, false)
	if err != nil {
		return radioPlaylist{}, err
	}
	return parseRadioPlaylist(b, base)
}

type radioWindow struct {
	url  *url.URL
	list radioPlaylist
}

func resolveRadio(ctx context.Context, client *http.Client, stationID string) ([]radioRendition, error) {
	directory := &url.URL{Scheme: "https", Host: tuneInHost, Path: "/Tune.ashx", RawQuery: url.Values{
		"id": {tuneInStations[stationID]}, "render": {"json"}, "formats": {"mp3,aac,hls"},
	}.Encode()}
	b, _, err := getRadio(ctx, client, directory, radioListLimit, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Body []struct{ URL string } `json:"body"`
	}
	if json.Unmarshal(b, &response) != nil {
		return nil, errors.New("invalid broadcast directory")
	}
	last := errors.New("no supported broadcast URL")
	for _, entry := range response.Body {
		u, err := url.Parse(entry.URL)
		if err != nil || !radioURLAllowed(u, false) {
			continue
		}
		master, base, err := getRadio(ctx, client, u, radioListLimit, false)
		if err != nil {
			last = err
			continue
		}
		renditions, err := radioRenditions(master, base)
		if err == nil {
			return renditions, nil
		}
		last = err
	}
	return nil, fmt.Errorf("broadcast playlist unavailable: %w", last)
}

// PreparedRadio holds a recently verified, in-memory starting segment. It
// contains no playback credentials and can be discarded without cleanup.
type PreparedRadio struct {
	id       string
	client   *http.Client
	window   radioWindow
	sequence int64
	audio    []byte
	until    time.Time
}

func PrepareRadio(ctx context.Context, stationID string) (*PreparedRadio, error) {
	return prepareRadio(ctx, radioHTTP, stationID)
}

func prepareRadio(ctx context.Context, client *http.Client, stationID string) (*PreparedRadio, error) {
	if !codec.HasAAC || tuneInStations[stationID] == "" {
		return nil, errors.New("unsupported")
	}
	ctx, cancel := context.WithTimeout(ctx, radioTimeout)
	defer cancel()
	prepared, err := openRadio(ctx, client, stationID)
	if err != nil && waitRadio(ctx, 250*time.Millisecond) == nil {
		return openRadio(ctx, client, stationID)
	}
	return prepared, err
}

func openRadio(ctx context.Context, client *http.Client, stationID string) (*PreparedRadio, error) {
	renditions, err := resolveRadio(ctx, client, stationID)
	if err != nil {
		return nil, err
	}
	var failures []error
	for _, r := range renditions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		list, err := readRadioPlaylist(ctx, client, r.url)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		first := list.start()
		b, _, err := getRadio(ctx, client, list.segs[first-list.first].url, radioAudioLimit, false)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		var decoder radioDecoder
		err = decoder.decode(ctx, b, false, nil, nil)
		decoder.close()
		if err == nil {
			return &PreparedRadio{stationID, client, radioWindow{r.url, list}, first, b, time.Now().Add(20 * time.Second)}, nil
		}
		failures = append(failures, err)
	}
	return nil, fmt.Errorf("broadcast audio unavailable: %w", errors.Join(failures...))
}

func (p *PreparedRadio) ReadyFor(id string) bool {
	return p != nil && p.id == id && time.Now().Before(p.until)
}

type incomingRadio struct {
	audio []byte
	reset bool
	err   error
}

func waitRadio(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *PreparedRadio) refresh(ctx context.Context) (radioWindow, error) {
	ctx, cancel := context.WithTimeout(ctx, radioTimeout)
	defer cancel()
	rs, err := resolveRadio(ctx, p.client, p.id)
	if err != nil {
		return radioWindow{}, err
	}
	for _, r := range rs {
		list, err := readRadioPlaylist(ctx, p.client, r.url)
		if err == nil {
			return radioWindow{r.url, list}, nil
		}
	}
	return radioWindow{}, errors.New("broadcast playlist unavailable")
}

func (p *PreparedRadio) pump(ctx context.Context, out chan<- incomingRadio) error {
	window := p.window
	next := p.sequence + 1
	// A window may be temporarily stale at the CDN. Re-resolve it after
	// consecutive failures, or when its sequence stalls for several segments.
	failures, reopens := 0, 0
	lastAdvance := time.Now()
	reset := false
	for ctx.Err() == nil {
		if next < window.list.first {
			next, reset = window.list.start(), true
		}
		if i := next - window.list.first; i < int64(len(window.list.segs)) {
			seg := window.list.segs[i]
			b, _, err := getRadio(ctx, p.client, seg.url, radioAudioLimit, false)
			if err == nil {
				select {
				case out <- incomingRadio{audio: b, reset: reset || seg.discontinuity}:
				case <-ctx.Done():
					return ctx.Err()
				}
				next++
				failures, reopens, reset, lastAdvance = 0, 0, false, time.Now()
				continue
			}
			failures++
		}
		if failures >= 3 || time.Since(lastAdvance) > 3*window.list.target {
			if reopens >= 2 {
				return errors.New("broadcast stopped updating")
			}
			reopens++
			newWindow, err := p.refresh(ctx)
			if err == nil {
				if next > newWindow.list.first+int64(len(newWindow.list.segs)) {
					// The source restarted with a new sequence range.
					next, reset = newWindow.list.start(), true
				}
				window = newWindow
				failures, lastAdvance = 0, time.Now()
				continue
			}
		}
		delay := min(3*time.Second, max(500*time.Millisecond, window.list.target/2))
		if err := waitRadio(ctx, delay); err != nil {
			return err
		}
		list, err := readRadioPlaylist(ctx, p.client, window.url)
		if err != nil {
			failures++
			continue
		}
		window.list = list
	}
	return ctx.Err()
}

// Decode follows a sliding live window. Downloads overlap audio output, with
// at most one future segment held while the current one is consumed.
func (p *PreparedRadio) Decode(ctx context.Context, onFormat func(codec.Format) error, onPCM func([]byte) error) error {
	ctx, cancel := context.WithCancel(ctx)
	ready, done := make(chan incomingRadio), make(chan struct{})
	go func() {
		defer close(done)
		err := p.pump(ctx, ready)
		select {
		case ready <- incomingRadio{err: err}:
		case <-ctx.Done():
		}
	}()
	var decoder radioDecoder
	defer decoder.close()
	defer func() { cancel(); <-done }()
	if err := decoder.decode(ctx, p.audio, false, onFormat, onPCM); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item := <-ready:
			if item.err != nil {
				return item.err
			}
			if err := decoder.decode(ctx, item.audio, item.reset, onFormat, onPCM); err != nil {
				return err
			}
		}
	}
}
