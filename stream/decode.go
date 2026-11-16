// Package stream decrypts and decodes one bounded HLS segment at a time.
// Clear audio is delivered only to the caller's in-memory PCM sink.
package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/InkCitrus/apple-music-native-core/catalog"
	"github.com/InkCitrus/apple-music-native-core/codec"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Keys interface {
	OpenKey(string, string) error
	Decrypt([]byte) error
}

var mediaClient = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
	if len(via) > 5 || r.URL.Scheme != "https" {
		return errors.New("invalid media redirect")
	}
	return nil
}}

type mediaRange struct {
	URL, Key        string
	Offset, Length  int64
	Start, Duration float64
}
type track struct {
	Cookie []byte
	Sinf   *mp4.SinfBox
}

func attribute(line, name string) string {
	r := regexp.MustCompile(regexp.QuoteMeta(name) + `="([^"]*)"`)
	m := r.FindStringSubmatch(line)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}
func byteRange(s string) (int64, int64, error) {
	a := strings.Split(s, "@")
	if len(a) != 2 {
		return 0, 0, errors.New("explicit media byte range required")
	}
	length, e := strconv.ParseInt(a[0], 10, 64)
	if e != nil {
		return 0, 0, e
	}
	offset, e := strconv.ParseInt(a[1], 10, 64)
	if e != nil || length <= 0 || length > 32<<20 || offset < 0 || offset > math.MaxInt64-length {
		return 0, 0, errors.New("invalid media byte range")
	}
	return offset, length, nil
}
func playlist(p catalog.Plan) (mediaRange, []mediaRange, error) {
	base, e := url.Parse(p.MediaURL)
	if e != nil {
		return mediaRange{}, nil, errors.New("invalid playlist base")
	}
	resolve := func(s string) string {
		u, e := url.Parse(s)
		if e != nil {
			return ""
		}
		return base.ResolveReference(u).String()
	}
	var init mediaRange
	var segments []mediaRange
	var key, rangeText string
	var duration, position float64
	for _, line := range strings.Split(p.Playlist, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			key = strings.Split(attribute(line, "URI"), ",")[0]
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			o, n, e := byteRange(attribute(line, "BYTERANGE"))
			if e != nil {
				return init, nil, e
			}
			init = mediaRange{URL: resolve(attribute(line, "URI")), Offset: o, Length: n}
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"):
			rangeText = strings.TrimPrefix(line, "#EXT-X-BYTERANGE:")
		case strings.HasPrefix(line, "#EXTINF:"):
			duration, e = strconv.ParseFloat(strings.Split(strings.TrimPrefix(line, "#EXTINF:"), ",")[0], 64)
			if e != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
				return init, nil, errors.New("invalid media duration")
			}
		case line != "" && !strings.HasPrefix(line, "#"):
			o, n, e := byteRange(rangeText)
			if e != nil {
				return init, nil, e
			}
			segments = append(segments, mediaRange{URL: resolve(line), Key: key, Offset: o, Length: n, Start: position, Duration: duration})
			position += duration
			rangeText = ""
		}
	}
	if init.URL == "" || len(segments) == 0 {
		return init, nil, errors.New("empty media playlist")
	}
	return init, segments, nil
}
func fetch(ctx context.Context, r mediaRange) ([]byte, error) {
	u, ue := url.Parse(r.URL)
	if ue != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("invalid media URL")
	}
	request, e := http.NewRequestWithContext(ctx, "GET", r.URL, nil)
	if e != nil {
		return nil, errors.New("invalid media request")
	}
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", r.Offset, r.Offset+r.Length-1))
	request.Header.Set("Accept-Encoding", "identity")
	response, e := mediaClient.Do(request)
	if e != nil {
		return nil, errors.New("media network unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 206 {
		return nil, fmt.Errorf("media range status %d", response.StatusCode)
	}
	if !strings.HasPrefix(response.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-%d/", r.Offset, r.Offset+r.Length-1)) {
		return nil, errors.New("media range offset mismatch")
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, r.Length+1))
	if e != nil {
		return nil, errors.New("media read failed")
	}
	if int64(len(data)) != r.Length {
		return nil, errors.New("media range length mismatch")
	}
	return data, nil
}
func decodeInit(data []byte) ([]track, *mp4.TrexBox, error) {
	reader := bytes.NewReader(data)
	var offset uint64
	var moov *mp4.MoovBox
	for reader.Len() > 0 {
		box, e := mp4.DecodeBox(offset, reader)
		if e != nil {
			return nil, nil, e
		}
		offset += box.Size()
		if b, ok := box.(*mp4.MoovBox); ok {
			moov = b
		}
	}
	if moov == nil || len(moov.Traks) != 1 {
		return nil, nil, errors.New("one audio track required")
	}
	t := moov.Traks[0]
	var entries []track
	for _, box := range t.Mdia.Minf.Stbl.Stsd.Children {
		a, ok := box.(*mp4.AudioSampleEntryBox)
		if !ok {
			return nil, nil, errors.New("encrypted audio sample entry required")
		}
		var cookie []byte
		for _, child := range a.Children {
			if child.Type() == "alac" {
				u, ok := child.(*mp4.UnknownBox)
				if ok {
					body := u.Payload()
					if len(body) >= 28 {
						cookie = append([]byte(nil), body[4:28]...)
					}
				}
			}
		}
		if len(cookie) != 24 || a.Sinf == nil || a.Sinf.Schm == nil || a.Sinf.Schm.SchemeType != "cbcs" || a.Sinf.Schi == nil || a.Sinf.Schi.Tenc == nil {
			return nil, nil, errors.New("ALAC CBCS configuration missing")
		}
		entries = append(entries, track{cookie, a.Sinf})
	}
	if len(entries) == 0 {
		return nil, nil, errors.New("audio configuration missing")
	}
	for _, e := range entries {
		if !bytes.Equal(e.Cookie, entries[0].Cookie) {
			return nil, nil, errors.New("changing ALAC format unsupported")
		}
	}
	trex := mp4.CreateTrex(t.Tkhd.TrackID)
	trex.DefaultSampleDuration = 4096
	if moov.Mvex != nil {
		for _, v := range moov.Mvex.Trexs {
			if v.TrackID == t.Tkhd.TrackID {
				trex = v
			}
		}
	}
	return entries, trex, nil
}
func decryptSample(data []byte, patterns []mp4.SubSamplePattern, tenc *mp4.TencBox, keys Keys) error {
	if tenc.DefaultSkipByteBlock != 0 {
		return errors.New("non-ALAC encryption pattern")
	}
	if len(patterns) == 0 {
		return keys.Decrypt(data)
	}
	var pos uint64
	for _, p := range patterns {
		pos += uint64(p.BytesOfClearData)
		end := pos + uint64(p.BytesOfProtectedData)
		if end > uint64(len(data)) {
			return errors.New("invalid subsample bounds")
		}
		if e := keys.Decrypt(data[pos:end]); e != nil {
			return e
		}
		pos = end
	}
	return nil
}
func samples(ctx context.Context, emit func([]byte) error, format codec.Format, data []byte, offset uint64, entries []track, trex *mp4.TrexBox, keys Keys, decoder *codec.Decoder, trim *int64) error {
	reader := bytes.NewReader(data)
	var fragment *mp4.Fragment
	for reader.Len() > 0 {
		box, e := mp4.DecodeBox(offset, reader)
		if e != nil {
			return e
		}
		offset += box.Size()
		switch box.Type() {
		case "moof":
			fragment = mp4.NewFragment()
			fragment.AddChild(box)
		case "mdat":
			if fragment == nil {
				return errors.New("audio data without metadata")
			}
			fragment.AddChild(box)
			traf := fragment.Moof.Traf
			if traf == nil || traf.Tfhd == nil {
				return errors.New("fragment track metadata missing")
			}
			index := trex.DefaultSampleDescriptionIndex
			if index == 0 {
				index = 1
			}
			if traf.Tfhd.HasSampleDescriptionIndex() {
				index = traf.Tfhd.SampleDescriptionIndex
			}
			if index == 0 || int(index) > len(entries) {
				return errors.New("invalid sample description index")
			}
			entry := entries[index-1]
			tenc := entry.Sinf.Schi.Tenc
			has, parsed := traf.ContainsSencBox()
			if !has {
				return errors.New("encrypted samples lack encryption metadata")
			}
			if !parsed {
				if e := traf.ParseReadSenc(tenc.DefaultPerSampleIVSize, fragment.Moof.StartPos); e != nil {
					return e
				}
			}
			senc := traf.Senc
			if senc == nil && traf.UUIDSenc != nil {
				senc = traf.UUIDSenc.Senc
			}
			if senc == nil {
				return errors.New("sample encryption data missing")
			}
			full, e := fragment.GetFullSamples(trex)
			if e != nil {
				return e
			}
			if len(senc.SubSamples) > 0 && len(senc.SubSamples) < len(full) {
				return errors.New("incomplete subsample table")
			}
			for i, s := range full {
				var sub []mp4.SubSamplePattern
				if len(senc.SubSamples) > 0 {
					sub = senc.SubSamples[i]
				}
				if e := decryptSample(s.Data, sub, tenc, keys); e != nil {
					return fmt.Errorf("native sample decryption failed: %w", e)
				}
				pcm, e := decoder.Decode(s.Data)
				if e != nil {
					return e
				}
				frames := len(pcm) / (int(format.Channels) * int(format.Bits/8))
				if s.Dur != 0 && uint32(frames) != s.Dur {
					return errors.New("ALAC frames differ from sample duration")
				}
				if *trim >= int64(frames) {
					*trim -= int64(frames)
					continue
				}
				if *trim > 0 {
					pcm = pcm[int(*trim)*int(format.Channels)*int(format.Bits/8):]
					*trim = 0
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := emit(pcm); err != nil {
					return err
				}

			}
			fragment = nil
		}
	}
	return nil
}

// Decode streams ALAC PCM from start seconds. onFormat is called once before PCM.
// All callbacks run on the calling goroutine and may provide backpressure.
func Decode(ctx context.Context, p catalog.Plan, start float64, keys Keys, onFormat func(codec.Format) error, emit func([]byte) error) error {
	init, segments, err := playlist(p)
	if err != nil {
		return err
	}
	last := segments[len(segments)-1]
	if math.IsNaN(start) || math.IsInf(start, 0) || start < 0 || start >= last.Start+last.Duration {
		return errors.New("seek outside track")
	}
	initial, err := fetch(ctx, init)
	if err != nil {
		return err
	}
	entries, trex, err := decodeInit(initial)
	if err != nil {
		return err
	}
	decoder, err := codec.New(entries[0].Cookie)
	if err != nil {
		return err
	}
	defer decoder.Close()
	format := decoder.Format()
	if err = onFormat(format); err != nil {
		return err
	}
	lastKey := ""
	var trim int64
	first := true
	for _, segment := range segments {
		if err = ctx.Err(); err != nil {
			return err
		}
		if segment.Start+segment.Duration <= start {
			continue
		}
		if first {
			trim = int64(math.Round((start - segment.Start) * float64(format.SampleRate)))
			if trim < 0 {
				trim = 0
			}
			first = false
		}
		if segment.Key != lastKey {
			id := p.TrackID
			if strings.Contains(segment.Key, "P000000000") {
				id = "0"
			}
			if err = keys.OpenKey(id, segment.Key); err != nil {
				return err
			}
			lastKey = segment.Key
		}
		data, err := fetch(ctx, segment)
		if err != nil {
			return err
		}
		if err = samples(ctx, emit, format, data, uint64(segment.Offset), entries, trex, keys, decoder, &trim); err != nil {
			return err
		}
	}
	return nil
}
