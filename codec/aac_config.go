package codec

import "errors"

func aacLCFormat(config []byte) (Format, error) {
	rates := [...]uint32{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}
	if len(config) < 2 || len(config) > 64 || config[0]>>3 != 2 || config[1]&7 != 0 {
		return Format{}, errors.New("unsupported AAC-LC configuration")
	}
	rate := (config[0]&7)<<1 | config[1]>>7
	channels := (config[1] >> 3) & 15
	if int(rate) >= len(rates) || channels == 0 || channels > 2 {
		return Format{}, errors.New("unsupported AAC sample rate or channels")
	}
	return Format{Codec: "aac", SampleRate: rates[rate], Channels: channels, Bits: 16, FramesPerPacket: 1024}, nil
}
