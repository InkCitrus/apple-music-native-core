package audio

// Stats contains output health counters only; it contains no account/media data.
type Stats struct {
	Underruns      uint64 `json:"underruns"`
	QueuedFrames   uint64 `json:"queuedFrames"`
	CapacityFrames uint64 `json:"capacityFrames"`
}
