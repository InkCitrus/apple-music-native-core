// Package catalog resolves licensed ALAC streams using a native subscriber session.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Tokens struct {
	Developer string `json:"developerToken"`
	User      string `json:"mediaUserToken"`
}

func LoadTokens(profile string) (Tokens, error) {
	var t Tokens
	for name, target := range map[string]*string{"DEVELOPER_TOKEN": &t.Developer, "MUSIC_TOKEN": &t.User} {
		f, e := os.Open(filepath.Join(profile, name))
		if e != nil {
			return t, errors.New("signIn")
		}
		data, e := io.ReadAll(io.LimitReader(f, 65537))
		f.Close()
		if e != nil || len(data) > 65536 || len(strings.TrimSpace(string(data))) == 0 {
			return t, errors.New("signIn")
		}
		*target = strings.TrimSpace(string(data))
	}
	return t, nil
}

type Plan struct {
	TrackID    string `json:"trackID"`
	Title      string `json:"title"`
	DurationMs int64  `json:"durationMs"`
	MediaURL   string `json:"mediaURL"`
	Playlist   string `json:"playlist"`
}
type Client struct {
	HTTP   *http.Client
	Tokens Tokens
}

func New(t Tokens) *Client { return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}, Tokens: t} }

// CDNClient never sends subscriber tokens, including across redirects.
func (c *Client) Get(ctx context.Context, raw string, authenticated bool) ([]byte, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("invalid media URL")
	}
	if authenticated && u.Hostname() != "amp-api.music.apple.com" {
		return nil, errors.New("invalid API host")
	}
	r, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return nil, errors.New("invalid request")
	}
	if authenticated {
		r.Header.Set("Authorization", "Bearer "+c.Tokens.Developer)
		r.Header.Set("Music-User-Token", c.Tokens.User)
		r.Header.Set("Origin", "https://music.apple.com")
	}
	hc := *c.HTTP
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("redirect limit")
		}
		if req.URL.Scheme != "https" || authenticated && req.URL.Hostname() != "amp-api.music.apple.com" {
			return errors.New("invalid redirect")
		}
		return nil
	}
	response, e := hc.Do(r)
	if e != nil {
		return nil, errors.New("network unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return nil, errors.New("signIn")
	}
	if response.StatusCode != 200 {
		return nil, errors.New("catalog unavailable")
	}
	b, e := io.ReadAll(io.LimitReader(response.Body, 3<<20+1))
	if e != nil || len(b) > 3<<20 {
		return nil, errors.New("response too large")
	}
	return b, nil
}
func (c *Client) API(ctx context.Context, path string, out any) error {
	b, e := c.Get(ctx, "https://amp-api.music.apple.com/v1/"+path, true)
	if e != nil {
		return e
	}
	if json.Unmarshal(b, out) != nil {
		return errors.New("invalid catalog response")
	}
	return nil
}
func (c *Client) Storefront(ctx context.Context) (string, error) {
	var v struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if e := c.API(ctx, "me/storefront", &v); e != nil {
		return "", e
	}
	if len(v.Data) != 1 || !regexp.MustCompile(`^[a-z]{2}$`).MatchString(v.Data[0].ID) {
		return "", errors.New("invalid storefront")
	}
	return v.Data[0].ID, nil
}

var numericID = regexp.MustCompile(`^[0-9]{1,24}$`)

func (c *Client) Resolve(ctx context.Context, id, quality string) (Plan, error) {
	var p Plan
	if !numericID.MatchString(id) {
		return p, errors.New("unsupported track: a catalog song is required")
	}
	sf, e := c.Storefront(ctx)
	if e != nil {
		return p, e
	}
	var v struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Name     string `json:"name"`
				Duration int64  `json:"durationInMillis"`
				Assets   struct {
					HLS string `json:"enhancedHls"`
				} `json:"extendedAssetUrls"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if e = c.API(ctx, "catalog/"+sf+"/songs/"+id+"?extend=extendedAssetUrls", &v); e != nil {
		return p, e
	}
	if len(v.Data) != 1 || v.Data[0].Attributes.Assets.HLS == "" {
		return p, errors.New("lossless unavailable for this track")
	}
	song := v.Data[0]
	master := song.Attributes.Assets.HLS
	b, e := c.Get(ctx, master, false)
	if e != nil {
		return p, e
	}
	media, e := SelectALAC(master, string(b), quality)
	if e != nil {
		return p, e
	}
	b, e = c.Get(ctx, media, false)
	if e != nil {
		return p, e
	}
	return Plan{song.ID, song.Attributes.Name, song.Attributes.Duration, media, string(b)}, nil
}

type variant struct {
	url                   string
	rate, bits, bandwidth int
}

func Attribute(line, name string) string {
	m := regexp.MustCompile(`(?:^|[:,])` + regexp.QuoteMeta(name) + `=(?:"([^"]*)"|([^,]*))`).FindStringSubmatch(line)
	if len(m) < 3 {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}
func SelectALAC(base, body, quality string) (string, error) {
	u, e := url.Parse(base)
	if e != nil {
		return "", errors.New("invalid master playlist")
	}
	lines := strings.Split(body, "\n")
	var variants []variant
	for i, line := range lines {
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF:") || !strings.Contains(strings.ToLower(Attribute(line, "CODECS")), "alac") {
			continue
		}
		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}
		if j >= len(lines) || strings.HasPrefix(strings.TrimSpace(lines[j]), "#") {
			continue
		}
		ref, e := url.Parse(strings.TrimSpace(lines[j]))
		if e != nil {
			continue
		}
		rate, _ := strconv.Atoi(Attribute(line, "SAMPLE-RATE"))
		bits, _ := strconv.Atoi(Attribute(line, "BIT-DEPTH"))
		bw, _ := strconv.Atoi(Attribute(line, "AVERAGE-BANDWIDTH"))
		if bw == 0 {
			bw, _ = strconv.Atoi(Attribute(line, "BANDWIDTH"))
		}
		// Apple variants also encode these values in the stable audio group name.
		group := regexp.MustCompile(`audio-alac-(?:stereo-|mono-)?(\d+)-(\d+)`).FindStringSubmatch(Attribute(line, "AUDIO"))
		if len(group) == 3 {
			if rate == 0 {
				rate, _ = strconv.Atoi(group[1])
			}
			if bits == 0 {
				bits, _ = strconv.Atoi(group[2])
			}
		}
		if quality == "lossless" && rate > 48000 {
			continue
		}
		variants = append(variants, variant{u.ResolveReference(ref).String(), rate, bits, bw})
	}
	if len(variants) == 0 {
		return "", errors.New("requested ALAC quality unavailable")
	}
	sort.SliceStable(variants, func(i, j int) bool {
		a, b := variants[i], variants[j]
		if a.rate != b.rate {
			return a.rate > b.rate
		}
		if a.bits != b.bits {
			return a.bits > b.bits
		}
		return a.bandwidth > b.bandwidth
	})
	return variants[0].url, nil
}
