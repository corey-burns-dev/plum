package transcoder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"plum/internal/db"
)

type videoStreamInfo struct {
	CodecName string
	PixelFmt  string
}

type transcodePlan struct {
	Args         []string
	Mode         string
	EncodeFormat string
}

const hlsSegmentDurationSeconds = 2

func GetSettingsWarnings(settings db.TranscodingSettings) []db.TranscodingSettingsWarning {
	settings = db.NormalizeTranscodingSettings(settings)
	warnings := make([]db.TranscodingSettingsWarning, 0)

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return append(warnings, db.TranscodingSettingsWarning{
			Code:    "ffmpeg_missing",
			Message: "FFmpeg is not available on the server PATH.",
		})
	}

	if settings.VAAPIEnabled {
		if _, err := os.Stat(settings.VAAPIDevicePath); err != nil {
			warnings = append(warnings, db.TranscodingSettingsWarning{
				Code:    "vaapi_device_unavailable",
				Message: fmt.Sprintf("VAAPI device %s is not accessible: %v", settings.VAAPIDevicePath, err),
			})
		}
		if !ffmpegCommandContains(context.Background(), "-hwaccels", "vaapi") {
			warnings = append(warnings, db.TranscodingSettingsWarning{
				Code:    "vaapi_unavailable",
				Message: "FFmpeg does not report VAAPI hardware acceleration support.",
			})
		}
	}

	if settings.VAAPIEnabled && settings.HardwareEncodingEnabled {
		encoders := map[string]string{
			"h264": "h264_vaapi",
			"hevc": "hevc_vaapi",
			"av1":  "av1_vaapi",
		}
		for format, encoder := range encoders {
			if !settings.EncodeFormats.Enabled(format) {
				continue
			}
			if !ffmpegCommandContains(context.Background(), "-encoders", encoder) {
				warnings = append(warnings, db.TranscodingSettingsWarning{
					Code:    "encoder_unavailable_" + format,
					Message: fmt.Sprintf("FFmpeg does not report %s for VAAPI encoding.", encoder),
				})
			}
		}
	}

	return warnings
}

func buildTranscodePlans(itemPath, outPath string, settings db.TranscodingSettings, stream videoStreamInfo, audioIndex int) []transcodePlan {
	settings = db.NormalizeTranscodingSettings(settings)

	plans := make([]transcodePlan, 0, 2)
	if hardwarePlan, ok := buildHardwarePlan(itemPath, outPath, settings, stream, audioIndex); ok {
		plans = append(plans, hardwarePlan)
	}
	plans = append(plans, buildSoftwarePlan(itemPath, outPath, settings, audioIndex))
	return plans
}

func buildHLSPlans(itemPath, outDir string, settings db.TranscodingSettings, stream videoStreamInfo, audioIndex int) []transcodePlan {
	settings = db.NormalizeTranscodingSettings(settings)

	plans := make([]transcodePlan, 0, 2)
	if hardwarePlan, ok := buildHardwareHLSPlan(itemPath, outDir, settings, stream, audioIndex); ok {
		plans = append(plans, hardwarePlan)
	}
	plans = append(plans, buildSoftwareHLSPlan(itemPath, outDir, settings, audioIndex))
	return plans
}

func buildHardwarePlan(itemPath, outPath string, settings db.TranscodingSettings, stream videoStreamInfo, audioIndex int) (transcodePlan, bool) {
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

	if decodeCodec := detectVAAPIDecodeCodec(stream); settings.DecodeCodecs.Enabled(decodeCodec) {
		args = append(args,
			"-hwaccel", "vaapi",
			"-hwaccel_device", settings.VAAPIDevicePath,
			"-hwaccel_output_format", "vaapi",
			"-i", itemPath,
			"-vf", "scale_vaapi=format="+uploadFormat,
		)
	} else {
		args = append(args,
			"-i", itemPath,
			"-vf", "format="+uploadFormat+",hwupload",
		)
	}

	// Always map the first video stream.
	args = append(args, "-map", "0:v:0")

	// Map audio stream: specific index if provided, otherwise the first audio stream.
	if audioIndex >= 0 {
		args = append(args, "-map", fmt.Sprintf("0:%d", audioIndex))
	} else {
		args = append(args, "-map", "0:a:0?")
	}

	args = append(args,
		"-c:v", encoder,
		"-c:a", "aac",
		"-b:a", settings.AudioBitrate,
	)

	// Stereo downmix (0 = passthrough).
	if settings.AudioChannels > 0 {
		args = append(args, "-ac", strconv.Itoa(settings.AudioChannels))
	}

	// Keyframe interval for fast seeking.
	g := strconv.Itoa(settings.KeyframeInterval)
	args = append(args, "-g", g, "-keyint_min", g)

	// Optional max bitrate / buffer size.
	if settings.MaxBitrate != "" {
		args = append(args, "-maxrate", settings.MaxBitrate, "-bufsize", settings.MaxBitrate)
	}

	args = append(args,
		"-movflags", "+faststart",
		"-f", "mp4",
		outPath,
	)

	return transcodePlan{
		Args:         args,
		Mode:         "hardware",
		EncodeFormat: format,
	}, true
}

func buildSoftwarePlan(itemPath, outPath string, settings db.TranscodingSettings, audioIndex int) transcodePlan {
	settings = db.NormalizeTranscodingSettings(settings)

	args := []string{
		"-y",
		"-i", itemPath,
		"-map", "0:v:0",
	}

	if audioIndex >= 0 {
		args = append(args, "-map", fmt.Sprintf("0:%d", audioIndex))
	} else {
		args = append(args, "-map", "0:a:0?")
	}

	args = append(args,
		"-c:v", "libx264",
		"-crf", strconv.Itoa(settings.CRF),
		"-preset", "veryfast",
		"-pix_fmt", "yuv420p",
		"-profile:v", "high",
		"-c:a", "aac",
		"-b:a", settings.AudioBitrate,
	)

	// Stereo downmix (0 = passthrough).
	if settings.AudioChannels > 0 {
		args = append(args, "-ac", strconv.Itoa(settings.AudioChannels))
	}

	// Thread control (0 = auto).
	if settings.Threads > 0 {
		args = append(args, "-threads", strconv.Itoa(settings.Threads))
	}

	// Keyframe interval for fast seeking.
	g := strconv.Itoa(settings.KeyframeInterval)
	args = append(args, "-g", g, "-keyint_min", g)

	// Optional max bitrate / buffer size.
	if settings.MaxBitrate != "" {
		args = append(args, "-maxrate", settings.MaxBitrate, "-bufsize", settings.MaxBitrate)
	}

	args = append(args,
		"-movflags", "+faststart",
		"-f", "mp4",
		outPath,
	)

	return transcodePlan{
		Args:         args,
		Mode:         "software",
		EncodeFormat: "h264",
	}
}

func buildHardwareHLSPlan(itemPath, outDir string, settings db.TranscodingSettings, stream videoStreamInfo, audioIndex int) (transcodePlan, bool) {
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

	if decodeCodec := detectVAAPIDecodeCodec(stream); settings.DecodeCodecs.Enabled(decodeCodec) {
		args = append(args,
			"-hwaccel", "vaapi",
			"-hwaccel_device", settings.VAAPIDevicePath,
			"-hwaccel_output_format", "vaapi",
			"-i", itemPath,
			"-vf", "scale_vaapi=format="+uploadFormat,
		)
	} else {
		args = append(args,
			"-i", itemPath,
			"-vf", "format="+uploadFormat+",hwupload",
		)
	}

	args = append(args, "-map", "0:v:0")
	if audioIndex >= 0 {
		args = append(args, "-map", fmt.Sprintf("0:%d", audioIndex))
	} else {
		args = append(args, "-map", "0:a:0?")
	}

	args = append(args,
		"-c:v", encoder,
		"-c:a", "aac",
		"-b:a", settings.AudioBitrate,
	)
	if settings.AudioChannels > 0 {
		args = append(args, "-ac", strconv.Itoa(settings.AudioChannels))
	}
	g := strconv.Itoa(settings.KeyframeInterval)
	args = append(args, "-g", g, "-keyint_min", g)
	if settings.MaxBitrate != "" {
		args = append(args, "-maxrate", settings.MaxBitrate, "-bufsize", settings.MaxBitrate)
	}

	args = appendHLSOutputArgs(args, outDir)

	return transcodePlan{
		Args:         args,
		Mode:         "hardware",
		EncodeFormat: format,
	}, true
}

func buildSoftwareHLSPlan(itemPath, outDir string, settings db.TranscodingSettings, audioIndex int) transcodePlan {
	args := []string{
		"-y",
		"-i", itemPath,
		"-map", "0:v:0",
	}
	if audioIndex >= 0 {
		args = append(args, "-map", fmt.Sprintf("0:%d", audioIndex))
	} else {
		args = append(args, "-map", "0:a:0?")
	}

	args = append(args,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", strconv.Itoa(settings.CRF),
		"-pix_fmt", "yuv420p",
		"-profile:v", "high",
		"-c:a", "aac",
		"-b:a", settings.AudioBitrate,
	)
	if settings.AudioChannels > 0 {
		args = append(args, "-ac", strconv.Itoa(settings.AudioChannels))
	}
	if settings.Threads > 0 {
		args = append(args, "-threads", strconv.Itoa(settings.Threads))
	}
	g := strconv.Itoa(settings.KeyframeInterval)
	args = append(args, "-g", g, "-keyint_min", g)
	if settings.MaxBitrate != "" {
		args = append(args, "-maxrate", settings.MaxBitrate, "-bufsize", settings.MaxBitrate)
	}
	args = append(args,
		"-sc_threshold", "0",
	)
	args = appendHLSOutputArgs(args, outDir)

	return transcodePlan{
		Args:         args,
		Mode:         "software",
		EncodeFormat: "h264",
	}
}

func appendHLSOutputArgs(args []string, outDir string) []string {
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

func pickHardwareEncodeFormat(settings db.TranscodingSettings) string {
	if settings.EncodeFormats.Enabled(settings.PreferredHardwareEncodeFormat) {
		return settings.PreferredHardwareEncodeFormat
	}
	switch {
	case settings.EncodeFormats.H264:
		return "h264"
	case settings.EncodeFormats.HEVC:
		return "hevc"
	case settings.EncodeFormats.AV1:
		return "av1"
	default:
		return ""
	}
}

func hardwareEncoderName(format string) string {
	switch format {
	case "hevc":
		return "hevc_vaapi"
	case "av1":
		return "av1_vaapi"
	default:
		return "h264_vaapi"
	}
}

func hardwareUploadPixelFormat(format string, stream videoStreamInfo) string {
	if isTenBitStream(stream) && format != "h264" {
		return "p010"
	}
	return "nv12"
}

func detectVAAPIDecodeCodec(stream videoStreamInfo) string {
	switch strings.ToLower(stream.CodecName) {
	case "h264":
		return "h264"
	case "hevc":
		if isTenBitStream(stream) {
			return "hevc10bit"
		}
		return "hevc"
	case "mpeg2video":
		return "mpeg2"
	case "vc1":
		return "vc1"
	case "vp8":
		return "vp8"
	case "vp9":
		if isTenBitStream(stream) {
			return "vp910bit"
		}
		return "vp9"
	case "av1":
		return "av1"
	default:
		return ""
	}
}

func isTenBitStream(stream videoStreamInfo) bool {
	pixFmt := strings.ToLower(stream.PixelFmt)
	return strings.Contains(pixFmt, "10") || strings.Contains(pixFmt, "p010")
}

func probeVideoStream(path string) videoStreamInfo {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return videoStreamInfo{}
	}
	probe, err := probePlaybackSource(context.Background(), path)
	if err != nil {
		return videoStreamInfo{}
	}
	return probe.videoStreamInfo()
}

func ffmpegCommandContains(ctx context.Context, arg string, needle string) bool {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", arg)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(needle))
}
