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
	Codec     string
	Cookie    []byte
	Sinf      *mp4.SinfBox
	Timescale uint32
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
			// Each KEYFORMAT has its own key context. A Widevine or PlayReady
			// declaration must not replace the native FairPlay key for this segment.
			format := catalog.Attribute(line, "KEYFORMAT")
			uri := attribute(line, "URI")
			if format == "com.apple.streamingkeydelivery" || format == "" && strings.HasPrefix(uri, "skd://") {
				if catalog.Attribute(line, "METHOD") != "SAMPLE-AES" || !strings.HasPrefix(uri, "skd://") {
					return init, nil, errors.New("unsupported native key format")
				}
				key = strings.Split(uri, ",")[0]
			}
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
			if key == "" {
				return init, nil, errors.New("native FairPlay key unavailable")
			}
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

type prefetchedSegment struct {
	media mediaRange
	data  []byte
	err   error
}

// The unbuffered handoff permits only the current segment and one encrypted
// segment ahead. Network I/O overlaps decoding/output instead of starting only
// when the output queue is almost empty. Cancellation stops an in-flight fetch.
func prefetch(ctx context.Context, segments []mediaRange) (<-chan prefetchedSegment, <-chan struct{}) {
	ready := make(chan prefetchedSegment)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(ready)
		for _, segment := range segments {
			if ctx.Err() != nil {
				return
			}
			data, err := fetch(ctx, segment)
			select {
			case ready <- prefetchedSegment{segment, data, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return ready, done
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
	if t.Tkhd == nil || t.Mdia == nil || t.Mdia.Mdhd == nil || t.Mdia.Minf == nil || t.Mdia.Minf.Stbl == nil || t.Mdia.Minf.Stbl.Stsd == nil {
		return nil, nil, errors.New("audio track metadata missing")
	}
	var entries []track
	for _, box := range t.Mdia.Minf.Stbl.Stsd.Children {
		a, ok := box.(*mp4.AudioSampleEntryBox)
		if !ok || a.Sinf == nil || a.Sinf.Schm == nil || a.Sinf.Schm.SchemeType != "cbcs" || a.Sinf.Schi == nil || a.Sinf.Schi.Tenc == nil {
			return nil, nil, errors.New("encrypted audio sample entry required")
		}
		entry := track{Codec: "alac", Sinf: a.Sinf, Timescale: t.Mdia.Mdhd.Timescale}
		if a.Sinf.Frma != nil && a.Sinf.Frma.DataFormat == "mp4a" {
			entry.Codec = "aac"
			if a.Esds == nil || a.Esds.DecConfigDescriptor == nil || a.Esds.DecConfigDescriptor.ObjectType != 0x40 || a.Esds.DecConfigDescriptor.DecSpecificInfo == nil {
				return nil, nil, errors.New("AAC configuration missing")
			}
			entry.Cookie = a.Esds.DecConfigDescriptor.DecSpecificInfo.DecConfig
			entries = append(entries, entry)
			continue
		}
		for _, child := range a.Children {
			if child.Type() == "alac" {
				u, ok := child.(*mp4.UnknownBox)
				if ok {
					body := u.Payload()
					if len(body) >= 28 {
						entry.Cookie = append([]byte(nil), body[4:28]...)
					}
				}
			}
		}
		if len(entry.Cookie) != 24 {
			return nil, nil, errors.New("ALAC CBCS configuration missing")
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, nil, errors.New("audio configuration missing")
	}
	for _, e := range entries {
		if e.Codec != entries[0].Codec || !bytes.Equal(e.Cookie, entries[0].Cookie) {
			return nil, nil, errors.New("changing audio format unsupported")
		}
	}
	trex := mp4.CreateTrex(t.Tkhd.TrackID)
	trex.DefaultSampleDuration = 4096
	if entries[0].Codec == "aac" {
		trex.DefaultSampleDuration = 1024
	}
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
		return errors.New("unsupported audio encryption pattern")
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
func samples(ctx context.Context, emit func([]byte) error, format codec.Format, data []byte, offset uint64, entries []track, trex *mp4.TrexBox, keys Keys, decoder codec.PCMDecoder, trim *int64) error {
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
				frameBytes := int(format.Channels) * int(format.Bits/8)
				frames := len(pcm) / frameBytes
				if s.Dur != 0 && uint32(frames) != s.Dur {
					if format.Codec != "aac" || s.Dur > uint32(frames) {
						return errors.New("decoded frames differ from sample duration")
					}
					// The final AAC packet can have less playable audio than a full frame.
					frames = int(s.Dur)
					pcm = pcm[:frames*frameBytes]
				}
				if *trim >= int64(frames) {
					*trim -= int64(frames)
					continue
				}
				if *trim > 0 {
					pcm = pcm[int(*trim)*frameBytes:]
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

type Prepared struct {
	trackID        string
	start          float64
	segments       []mediaRange
	initial, first []byte
}

func Prepare(ctx context.Context, p catalog.Plan, start float64, byteLimit int64) (*Prepared, error) {
	init, segments, err := playlist(p)
	if err != nil {
		return nil, err
	}
	last := segments[len(segments)-1]
	if math.IsNaN(start) || math.IsInf(start, 0) || start < 0 || start >= last.Start+last.Duration {
		return nil, errors.New("seek outside track")
	}
	for segments[0].Start+segments[0].Duration <= start {
		segments = segments[1:]
	}
	if byteLimit > 0 && init.Length+segments[0].Length > byteLimit {
		return nil, errors.New("startup ranges exceed preparation limit")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		index int
		data  []byte
		err   error
	}
	completed := make(chan result, 2)
	for index, part := range []mediaRange{init, segments[0]} {
		go func() {
			data, e := fetch(ctx, part)
			completed <- result{index, data, e}
		}()
	}
	var data [2][]byte
	for range data {
		r := <-completed
		if r.err != nil && err == nil {
			err = r.err
			cancel()
		}
		data[r.index] = r.data
	}
	if err != nil {
		return nil, err
	}
	return &Prepared{trackID: p.TrackID, start: start, segments: segments, initial: data[0], first: data[1]}, nil
}

// Decode streams PCM from start seconds. onFormat is called once before PCM.
// All callbacks run on the calling goroutine and may provide backpressure.
func Decode(ctx context.Context, p catalog.Plan, start float64, keys Keys, onFormat func(codec.Format) error, emit func([]byte) error) error {
	input, err := Prepare(ctx, p, start, 0)
	if err != nil {
		return err
	}
	return input.Decode(ctx, keys, onFormat, emit)
}

func (p *Prepared) Decode(ctx context.Context, keys Keys, onFormat func(codec.Format) error, emit func([]byte) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, trex, err := decodeInit(p.initial)
	if err != nil {
		return err
	}
	var decoder codec.PCMDecoder
	if entries[0].Codec == "aac" {
		decoder, err = codec.NewAAC(entries[0].Cookie)
	} else {
		decoder, err = codec.New(entries[0].Cookie)
	}
	if err != nil {
		return err
	}
	defer decoder.Close()
	format := decoder.Format()
	if format.Codec == "aac" && entries[0].Timescale != format.SampleRate {
		return errors.New("unsupported AAC media timescale")
	}
	if err = onFormat(format); err != nil {
		return err
	}
	lastKey := ""
	first := p.segments[0]
	trim := max(0, int64(math.Round((p.start-first.Start)*float64(format.SampleRate))))
	consume := func(segment mediaRange, data []byte) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if segment.Key != lastKey {
			id := p.trackID
			if strings.Contains(segment.Key, "P000000000") {
				id = "0"
			}
			if e := keys.OpenKey(id, segment.Key); e != nil {
				return e
			}
			lastKey = segment.Key
		}
		return samples(ctx, emit, format, data, uint64(segment.Offset), entries, trex, keys, decoder, &trim)
	}
	ready, done := prefetch(ctx, p.segments[1:])
	defer func() { cancel(); <-done }()
	data := p.first
	p.initial, p.first = nil, nil
	if err = consume(first, data); err != nil {
		return err
	}
	data = nil
	for fetched := range ready {
		if fetched.err != nil {
			return fetched.err
		}
		if err = consume(fetched.media, fetched.data); err != nil {
			return err
		}
	}
	return ctx.Err()
}
