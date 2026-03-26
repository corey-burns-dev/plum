package transcoder

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"plum/internal/db"
)

type adaptiveVariant struct {
	Name      string
	Height    int
	VideoRate string
	MaxRate   string
	BufSize   string
}

func buildPlaybackHLSPlans(
	itemPath string,
	outDir string,
	settings db.TranscodingSettings,
	probe playbackSourceProbe,
	decision playbackDecision,
) []transcodePlan {
	switch decision.Delivery {
	case "remux":
		return []transcodePlan{buildRemuxHLSPlan(itemPath, outDir, settings, decision)}
	default:
		stream := probe.videoStreamInfo()
		variants := buildAdaptiveVariants(probe, settings)
		plans := make([]transcodePlan, 0, 2)
		if hardwarePlan, ok := buildHardwareAdaptiveHLSPlan(itemPath, outDir, settings, stream, decision, variants); ok {
			plans = append(plans, hardwarePlan)
		}
		plans = append(plans, buildSoftwareAdaptiveHLSPlan(itemPath, outDir, settings, decision, variants))
		return plans
	}
}

func buildRemuxHLSPlan(
	itemPath string,
	outDir string,
	settings db.TranscodingSettings,
	decision playbackDecision,
) transcodePlan {
	args := []string{
		"-y",
		"-i", itemPath,
		"-map", "0:v:0",
	}
	if decision.AudioIndex >= 0 {
		args = append(args, "-map", fmt.Sprintf("0:%d", decision.AudioIndex))
	} else {
		args = append(args, "-map", "0:a:0?")
	}

	args = append(args, "-c:v", "copy")
	if decision.AudioCopy {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args,
			"-c:a", "aac",
			"-b:a", settings.AudioBitrate,
		)
		if settings.AudioChannels > 0 {
			args = append(args, "-ac", strconv.Itoa(settings.AudioChannels))
		}
	}

	args = appendSingleVariantHLSOutputArgs(args, outDir)
	return transcodePlan{
		Args: args,
		Mode: "remux",
	}
}

func buildSoftwareAdaptiveHLSPlan(
	itemPath string,
	outDir string,
	settings db.TranscodingSettings,
	decision playbackDecision,
	variants []adaptiveVariant,
) transcodePlan {
	args := []string{
		"-y",
		"-i", itemPath,
	}

	filterParts := make([]string, 0, len(variants)+1)
	splitTargets := make([]string, 0, len(variants))
	for index := range variants {
		splitTargets = append(splitTargets, fmt.Sprintf("[v%d]", index))
	}
	filterParts = append(filterParts, fmt.Sprintf("[0:v:0]split=%d%s", len(variants), strings.Join(splitTargets, "")))
	for index, variant := range variants {
		filterParts = append(
			filterParts,
			fmt.Sprintf(
				"[v%d]scale=w=-2:h=%d:force_original_aspect_ratio=decrease,format=yuv420p[outv%d]",
				index,
				variant.Height,
				index,
			),
		)
		args = append(args, "-map", fmt.Sprintf("[outv%d]", index))
		if decision.AudioIndex >= 0 {
			args = append(args, "-map", fmt.Sprintf("0:%d", decision.AudioIndex))
		} else {
			args = append(args, "-map", "0:a:0?")
		}
	}

	args = append(args, "-filter_complex", strings.Join(filterParts, ";"))
	for index, variant := range variants {
		args = append(args,
			fmt.Sprintf("-c:v:%d", index), "libx264",
			fmt.Sprintf("-preset:v:%d", index), "veryfast",
			fmt.Sprintf("-crf:v:%d", index), strconv.Itoa(settings.CRF),
			fmt.Sprintf("-profile:v:%d", index), "high",
			fmt.Sprintf("-pix_fmt:v:%d", index), "yuv420p",
			fmt.Sprintf("-b:v:%d", index), variant.VideoRate,
			fmt.Sprintf("-maxrate:v:%d", index), variant.MaxRate,
			fmt.Sprintf("-bufsize:v:%d", index), variant.BufSize,
			fmt.Sprintf("-g:v:%d", index), strconv.Itoa(settings.KeyframeInterval),
			fmt.Sprintf("-keyint_min:v:%d", index), strconv.Itoa(settings.KeyframeInterval),
			fmt.Sprintf("-sc_threshold:v:%d", index), "0",
		)
		if decision.AudioCopy {
			args = append(args, fmt.Sprintf("-c:a:%d", index), "copy")
			continue
		}
		args = append(args,
			fmt.Sprintf("-c:a:%d", index), "aac",
			fmt.Sprintf("-b:a:%d", index), settings.AudioBitrate,
		)
		if settings.AudioChannels > 0 {
			args = append(args, fmt.Sprintf("-ac:%d", index), strconv.Itoa(settings.AudioChannels))
		}
	}

	args = appendAdaptiveHLSOutputArgs(args, outDir, variants)
	return transcodePlan{
		Args: args,
		Mode: "software",
	}
}

func buildHardwareAdaptiveHLSPlan(
	itemPath string,
	outDir string,
	settings db.TranscodingSettings,
	stream videoStreamInfo,
	decision playbackDecision,
	variants []adaptiveVariant,
) (transcodePlan, bool) {
	if !settings.VAAPIEnabled || !settings.HardwareEncodingEnabled || !settings.EncodeFormats.AnyEnabled() {
		return transcodePlan{}, false
	}

	format := pickHardwareEncodeFormat(settings)
	if format == "" {
		return transcodePlan{}, false
	}

	encoder := hardwareEncoderName(format)
	uploadFormat := hardwareUploadPixelFormat(format, stream)
	args := []string{"-y", "-vaapi_device", settings.VAAPIDevicePath}

	filterParts := make([]string, 0, len(variants)+2)
	splitTargets := make([]string, 0, len(variants))
	for index := range variants {
		splitTargets = append(splitTargets, fmt.Sprintf("[v%d]", index))
	}

	if decodeCodec := detectVAAPIDecodeCodec(stream); settings.DecodeCodecs.Enabled(decodeCodec) {
		args = append(args,
			"-hwaccel", "vaapi",
			"-hwaccel_device", settings.VAAPIDevicePath,
			"-hwaccel_output_format", "vaapi",
			"-i", itemPath,
		)
		filterParts = append(filterParts, fmt.Sprintf("[0:v:0]split=%d%s", len(variants), strings.Join(splitTargets, "")))
	} else {
		args = append(args, "-i", itemPath)
		filterParts = append(
			filterParts,
			fmt.Sprintf("[0:v:0]format=%s,hwupload,split=%d%s", uploadFormat, len(variants), strings.Join(splitTargets, "")),
		)
	}

	for index, variant := range variants {
		filterParts = append(
			filterParts,
			fmt.Sprintf(
				"[v%d]scale_vaapi=w=-2:h=%d:format=%s[outv%d]",
				index,
				variant.Height,
				uploadFormat,
				index,
			),
		)
		args = append(args, "-map", fmt.Sprintf("[outv%d]", index))
		if decision.AudioIndex >= 0 {
			args = append(args, "-map", fmt.Sprintf("0:%d", decision.AudioIndex))
		} else {
			args = append(args, "-map", "0:a:0?")
		}
	}

	args = append(args, "-filter_complex", strings.Join(filterParts, ";"))
	for index, variant := range variants {
		args = append(args,
			fmt.Sprintf("-c:v:%d", index), encoder,
			fmt.Sprintf("-b:v:%d", index), variant.VideoRate,
			fmt.Sprintf("-maxrate:v:%d", index), variant.MaxRate,
			fmt.Sprintf("-bufsize:v:%d", index), variant.BufSize,
			fmt.Sprintf("-g:v:%d", index), strconv.Itoa(settings.KeyframeInterval),
			fmt.Sprintf("-keyint_min:v:%d", index), strconv.Itoa(settings.KeyframeInterval),
		)
		if decision.AudioCopy {
			args = append(args, fmt.Sprintf("-c:a:%d", index), "copy")
			continue
		}
		args = append(args,
			fmt.Sprintf("-c:a:%d", index), "aac",
			fmt.Sprintf("-b:a:%d", index), settings.AudioBitrate,
		)
		if settings.AudioChannels > 0 {
			args = append(args, fmt.Sprintf("-ac:%d", index), strconv.Itoa(settings.AudioChannels))
		}
	}

	args = appendAdaptiveHLSOutputArgs(args, outDir, variants)
	return transcodePlan{
		Args:         args,
		Mode:         "hardware",
		EncodeFormat: format,
	}, true
}

func buildAdaptiveVariants(probe playbackSourceProbe, settings db.TranscodingSettings) []adaptiveVariant {
	video, ok := probe.primaryVideoStream()
	sourceHeight := 1080
	if ok && video.Height > 0 {
		sourceHeight = video.Height
	}
	ceiling := normalizedBitrateCeiling(settings.MaxBitrate)

	candidates := []adaptiveVariant{
		{Name: "1080p", Height: 1080, VideoRate: "8000k", MaxRate: "8560k", BufSize: "12000k"},
		{Name: "720p", Height: 720, VideoRate: "4000k", MaxRate: "4280k", BufSize: "6000k"},
		{Name: "480p", Height: 480, VideoRate: "2000k", MaxRate: "2140k", BufSize: "3000k"},
	}

	variants := make([]adaptiveVariant, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Height > sourceHeight {
			continue
		}
		if ceiling > 0 && parseBitRate(candidate.VideoRate) > ceiling {
			continue
		}
		variants = append(variants, candidate)
	}
	if len(variants) > 0 {
		return variants
	}

	fallback := adaptiveVariant{
		Name:      fmt.Sprintf("%dp", sourceHeight),
		Height:    sourceHeight,
		VideoRate: "2000k",
		MaxRate:   "2140k",
		BufSize:   "3000k",
	}
	if ceiling > 0 {
		fallback.VideoRate = formatBitRateKbps(ceiling)
		fallback.MaxRate = formatBitRateKbps(ceiling)
		fallback.BufSize = formatBitRateKbps(ceiling * 2)
	}
	return []adaptiveVariant{fallback}
}

func appendSingleVariantHLSOutputArgs(args []string, outDir string) []string {
	playlistPath := filepath.Join(outDir, "index.m3u8")
	segmentPath := filepath.Join(outDir, "segment_%05d.ts")
	return append(args,
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", hlsSegmentDurationSeconds),
		"-muxpreload", "0",
		"-muxdelay", "0",
		"-f", "hls",
		"-hls_time", strconv.Itoa(hlsSegmentDurationSeconds),
		"-hls_list_size", "0",
		"-hls_playlist_type", "event",
		"-hls_flags", "append_list+independent_segments+temp_file",
		"-hls_segment_filename", segmentPath,
		playlistPath,
	)
}

func appendAdaptiveHLSOutputArgs(args []string, outDir string, variants []adaptiveVariant) []string {
	varStreamEntries := make([]string, 0, len(variants))
	for index, variant := range variants {
		varStreamEntries = append(varStreamEntries, fmt.Sprintf("v:%d,a:%d,name:%s", index, index, variant.Name))
		_ = os.MkdirAll(filepath.Join(outDir, fmt.Sprintf("variant_%d", index)), 0o755)
	}
	return append(args,
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", hlsSegmentDurationSeconds),
		"-muxpreload", "0",
		"-muxdelay", "0",
		"-f", "hls",
		"-hls_time", strconv.Itoa(hlsSegmentDurationSeconds),
		"-hls_list_size", "0",
		"-hls_playlist_type", "event",
		"-hls_flags", "append_list+independent_segments+temp_file",
		"-master_pl_name", "index.m3u8",
		"-var_stream_map", strings.Join(varStreamEntries, " "),
		"-hls_segment_filename", filepath.Join(outDir, "variant_%v", "segment_%05d.ts"),
		filepath.Join(outDir, "variant_%v", "index.m3u8"),
	)
}

func normalizedBitrateCeiling(raw string) int64 {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "0" {
		return 0
	}

	last := trimmed[len(trimmed)-1]
	multiplier := int64(1)
	numeric := trimmed
	switch last {
	case 'k', 'K':
		multiplier = 1000
		numeric = trimmed[:len(trimmed)-1]
	case 'm', 'M':
		multiplier = 1000 * 1000
		numeric = trimmed[:len(trimmed)-1]
	}

	value, err := strconv.ParseFloat(numeric, 64)
	if err != nil || value <= 0 {
		return 0
	}
	return int64(value * float64(multiplier))
}

func formatBitRateKbps(value int64) string {
	if value <= 0 {
		return "0"
	}
	return fmt.Sprintf("%dk", value/1000)
}
