package transcoder

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

func buildTranscodePlans(itemPath, outPath string, settings db.TranscodingSettings, stream videoStreamInfo) []transcodePlan {
	settings = db.NormalizeTranscodingSettings(settings)

	plans := make([]transcodePlan, 0, 2)
	if hardwarePlan, ok := buildHardwarePlan(itemPath, outPath, settings, stream); ok {
		plans = append(plans, hardwarePlan)
	}
	plans = append(plans, buildSoftwarePlan(itemPath, outPath))
	return plans
}

func buildHardwarePlan(itemPath, outPath string, settings db.TranscodingSettings, stream videoStreamInfo) (transcodePlan, bool) {
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

	args = append(args,
		"-c:v", encoder,
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

func buildSoftwarePlan(itemPath, outPath string) transcodePlan {
	return transcodePlan{
		Args: []string{
			"-y",
			"-i", itemPath,
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-movflags", "+faststart",
			"-f", "mp4",
			outPath,
		},
		Mode:         "software",
		EncodeFormat: "h264",
	}
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

	cmd := exec.Command(
		"ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name,pix_fmt",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return videoStreamInfo{}
	}

	var payload struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
			PixelFmt  string `json:"pix_fmt"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &payload); err != nil || len(payload.Streams) == 0 {
		return videoStreamInfo{}
	}

	return videoStreamInfo{
		CodecName: payload.Streams[0].CodecName,
		PixelFmt:  payload.Streams[0].PixelFmt,
	}
}

func ffmpegCommandContains(ctx context.Context, arg string, needle string) bool {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", arg)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(needle))
}
