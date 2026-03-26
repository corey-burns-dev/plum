package transcoder

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var ffprobeCommandContext = exec.CommandContext

type playbackStreamProbe struct {
	Index     int
	CodecType string
	CodecName string
	PixelFmt  string
	Width     int
	Height    int
	Channels  int
	BitRate   int64
}

type playbackSourceProbe struct {
	Path      string
	Container string
	BitRate   int64
	Streams   []playbackStreamProbe
}

type playbackProbeCacheKey struct {
	path       string
	size       int64
	modUnixNano int64
}

type playbackProbeCache struct {
	mu    sync.RWMutex
	items map[playbackProbeCacheKey]playbackSourceProbe
}

var sourceProbeCache = playbackProbeCache{
	items: make(map[playbackProbeCacheKey]playbackSourceProbe),
}

func probePlaybackSource(ctx context.Context, path string) (playbackSourceProbe, error) {
	info, err := os.Stat(path)
	if err != nil {
		return playbackSourceProbe{}, err
	}

	key := playbackProbeCacheKey{
		path:        path,
		size:        info.Size(),
		modUnixNano: info.ModTime().UnixNano(),
	}

	sourceProbeCache.mu.RLock()
	cached, ok := sourceProbeCache.items[key]
	sourceProbeCache.mu.RUnlock()
	if ok {
		return cached, nil
	}

	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := ffprobeCommandContext(
		probeCtx,
		"ffprobe",
		"-v", "error",
		"-show_entries", "format=format_name,bit_rate:stream=index,codec_type,codec_name,pix_fmt,width,height,channels,bit_rate",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return playbackSourceProbe{}, err
	}

	var payload struct {
		Format struct {
			FormatName string `json:"format_name"`
			BitRate    string `json:"bit_rate"`
		} `json:"format"`
		Streams []struct {
			Index     int    `json:"index"`
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			PixelFmt  string `json:"pix_fmt"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Channels  int    `json:"channels"`
			BitRate   string `json:"bit_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return playbackSourceProbe{}, err
	}

	result := playbackSourceProbe{
		Path:      path,
		Container: normalizeContainerName(path, payload.Format.FormatName),
		BitRate:   parseBitRate(payload.Format.BitRate),
		Streams:   make([]playbackStreamProbe, 0, len(payload.Streams)),
	}

	for _, stream := range payload.Streams {
		result.Streams = append(result.Streams, playbackStreamProbe{
			Index:     stream.Index,
			CodecType: strings.ToLower(strings.TrimSpace(stream.CodecType)),
			CodecName: normalizeCodecName(stream.CodecName),
			PixelFmt:  strings.TrimSpace(stream.PixelFmt),
			Width:     stream.Width,
			Height:    stream.Height,
			Channels:  stream.Channels,
			BitRate:   parseBitRate(stream.BitRate),
		})
	}

	sourceProbeCache.mu.Lock()
	sourceProbeCache.items[key] = result
	sourceProbeCache.mu.Unlock()

	return result, nil
}

func parseBitRate(raw string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func normalizeContainerName(path string, formatName string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v", ".mov":
		return "mp4"
	case ".webm":
		return "webm"
	case ".mkv":
		return "mkv"
	}

	candidates := strings.Split(strings.ToLower(strings.TrimSpace(formatName)), ",")
	for _, candidate := range candidates {
		switch strings.TrimSpace(candidate) {
		case "mov", "mp4", "m4a", "3gp", "3g2", "mj2":
			return "mp4"
		case "matroska":
			return "mkv"
		case "webm":
			return "webm"
		case "mpegts":
			return "mpegts"
		case "avi":
			return "avi"
		case "ogg":
			return "ogg"
		}
	}

	if len(candidates) > 0 && strings.TrimSpace(candidates[0]) != "" {
		return strings.TrimSpace(candidates[0])
	}
	return "unknown"
}

func normalizeCodecName(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h265":
		return "hevc"
	case "avc":
		return "h264"
	case "mpeg2video":
		return "mpeg2"
	default:
		return strings.ToLower(strings.TrimSpace(codec))
	}
}

func (p playbackSourceProbe) primaryVideoStream() (playbackStreamProbe, bool) {
	for _, stream := range p.Streams {
		if stream.CodecType == "video" {
			return stream, true
		}
	}
	return playbackStreamProbe{}, false
}

func (p playbackSourceProbe) selectedAudioStream(audioIndex int) (playbackStreamProbe, bool) {
	if audioIndex >= 0 {
		for _, stream := range p.Streams {
			if stream.Index == audioIndex && stream.CodecType == "audio" {
				return stream, true
			}
		}
	}
	for _, stream := range p.Streams {
		if stream.CodecType == "audio" {
			return stream, true
		}
	}
	return playbackStreamProbe{}, false
}

func (p playbackSourceProbe) videoStreamInfo() videoStreamInfo {
	stream, ok := p.primaryVideoStream()
	if !ok {
		return videoStreamInfo{}
	}
	return videoStreamInfo{
		CodecName: stream.CodecName,
		PixelFmt:  stream.PixelFmt,
	}
}

func mediaStreamPath(mediaID int) string {
	return fmt.Sprintf("/api/stream/%d", mediaID)
}
