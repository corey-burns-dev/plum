package transcoder

import "strings"

type ClientPlaybackCapabilities struct {
	SupportsNativeHLS bool     `json:"supportsNativeHls"`
	SupportsMSEHLS    bool     `json:"supportsMseHls"`
	VideoCodecs       []string `json:"videoCodecs"`
	AudioCodecs       []string `json:"audioCodecs"`
	Containers        []string `json:"containers"`
}

func (c ClientPlaybackCapabilities) normalized() ClientPlaybackCapabilities {
	return ClientPlaybackCapabilities{
		SupportsNativeHLS: c.SupportsNativeHLS,
		SupportsMSEHLS:    c.SupportsMSEHLS,
		VideoCodecs:       normalizeCapabilityList(c.VideoCodecs),
		AudioCodecs:       normalizeCapabilityList(c.AudioCodecs),
		Containers:        normalizeContainerCapabilityList(c.Containers),
	}
}

func (c ClientPlaybackCapabilities) supportsHLS() bool {
	return c.SupportsNativeHLS || c.SupportsMSEHLS
}

type playbackDecision struct {
	Delivery       string
	StreamURL      string
	AudioIndex     int
	VideoCodec     string
	AudioCodec     string
	VideoCopy      bool
	AudioCopy      bool
	AudioTranscode bool
}

func decidePlayback(mediaID int, probe playbackSourceProbe, capabilities ClientPlaybackCapabilities, audioIndex int) playbackDecision {
	capabilities = capabilities.normalized()

	video, hasVideo := probe.primaryVideoStream()
	audio, hasAudio := probe.selectedAudioStream(audioIndex)

	videoCodec := ""
	audioCodec := ""
	if hasVideo {
		videoCodec = video.CodecName
	}
	if hasAudio {
		audioCodec = audio.CodecName
	}

	containerSupported := containsContainerCapability(capabilities.Containers, probe.Container)
	videoSupported := !hasVideo || containsCapability(capabilities.VideoCodecs, videoCodec)
	audioSupported := !hasAudio || containsCapability(capabilities.AudioCodecs, audioCodec)

	decision := playbackDecision{
		Delivery:   "transcode",
		StreamURL:  mediaStreamPath(mediaID),
		AudioIndex: audioIndex,
		VideoCodec: videoCodec,
		AudioCodec: audioCodec,
	}

	if containerSupported && videoSupported && audioSupported {
		decision.Delivery = "direct"
		return decision
	}

	if capabilities.supportsHLS() && videoSupported {
		decision.Delivery = "remux"
		decision.VideoCopy = true
		decision.AudioCopy = audioSupported && isHLSSafeAudioCodec(audioCodec)
		decision.AudioTranscode = hasAudio && !decision.AudioCopy
		return decision
	}

	decision.VideoCopy = videoSupported && isHLSSafeVideoCodec(videoCodec)
	decision.AudioCopy = audioSupported && isHLSSafeAudioCodec(audioCodec)
	decision.AudioTranscode = hasAudio && !decision.AudioCopy
	return decision
}

func normalizeCapabilityList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeCodecName(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func normalizeContainerCapabilityList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeCapabilityContainer(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func containsCapability(values []string, target string) bool {
	if target == "" {
		return false
	}
	normalizedTarget := normalizeCodecName(target)
	for _, value := range values {
		if value == normalizedTarget {
			return true
		}
	}
	return false
}

func containsContainerCapability(values []string, target string) bool {
	if target == "" {
		return false
	}
	normalizedTarget := normalizeCapabilityContainer(target)
	for _, value := range values {
		if value == normalizedTarget {
			return true
		}
	}
	return false
}

func isHLSSafeVideoCodec(codec string) bool {
	switch normalizeCodecName(codec) {
	case "h264", "hevc":
		return true
	default:
		return false
	}
}

func isHLSSafeAudioCodec(codec string) bool {
	switch normalizeCodecName(codec) {
	case "", "aac", "mp3", "ac3", "eac3":
		return true
	default:
		return false
	}
}

func normalizeCapabilityContainer(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "mov":
		return "mp4"
	default:
		return normalizeCodecName(value)
	}
}
