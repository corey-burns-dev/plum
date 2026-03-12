package transcoder

import (
	"fmt"
	"strings"
	"testing"

	"plum/internal/db"
)

func TestBuildTranscodePlans_SoftwareOnlyWhenHardwareDisabled(t *testing.T) {
	settings := db.DefaultTranscodingSettings()
	settings.VAAPIEnabled = false
	settings.HardwareEncodingEnabled = false
	stream := videoStreamInfo{CodecName: "h264", PixelFmt: "yuv420p"}

	plans := buildTranscodePlans("/media/test.mkv", "/tmp/out.mp4", settings, stream, -1)

	if len(plans) != 1 {
		t.Fatalf("plan count = %d", len(plans))
	}
	if plans[0].Mode != "software" {
		t.Fatalf("mode = %q", plans[0].Mode)
	}
	if !containsArgs(plans[0].Args, "libx264") {
		t.Fatalf("expected software encoder args: %v", plans[0].Args)
	}
}

func TestBuildTranscodePlans_UsesVAAPIDecodeForEnabledCodec(t *testing.T) {
	settings := db.DefaultTranscodingSettings()
	settings.VAAPIEnabled = true
	settings.HardwareEncodingEnabled = true
	stream := videoStreamInfo{CodecName: "h264", PixelFmt: "yuv420p"}

	plans := buildTranscodePlans("/media/test.mkv", "/tmp/out.mp4", settings, stream, -1)

	if len(plans) != 2 {
		t.Fatalf("plan count = %d", len(plans))
	}
	if plans[0].Mode != "hardware" {
		t.Fatalf("mode = %q", plans[0].Mode)
	}
	if !containsArgs(plans[0].Args, "h264_vaapi") {
		t.Fatalf("expected h264_vaapi encoder: %v", plans[0].Args)
	}
	if !containsArgs(plans[0].Args, "-hwaccel") || !containsFilter(plans[0].Args, "scale_vaapi=format=nv12") {
		t.Fatalf("expected vaapi decode path: %v", plans[0].Args)
	}
}

func TestBuildTranscodePlans_FallsBackToSoftwareDecodeWhenCodecDisabled(t *testing.T) {
	settings := db.DefaultTranscodingSettings()
	settings.VAAPIEnabled = true
	settings.HardwareEncodingEnabled = true
	settings.DecodeCodecs.HEVC10Bit = false
	settings.EncodeFormats.H264 = false
	settings.EncodeFormats.HEVC = true
	settings.PreferredHardwareEncodeFormat = "hevc"
	stream := videoStreamInfo{CodecName: "hevc", PixelFmt: "yuv420p10le"}

	plans := buildTranscodePlans("/media/test.mkv", "/tmp/out.mp4", settings, stream, -1)

	if len(plans) != 2 {
		t.Fatalf("plan count = %d", len(plans))
	}
	if !containsArgs(plans[0].Args, "hevc_vaapi") {
		t.Fatalf("expected hevc_vaapi encoder: %v", plans[0].Args)
	}
	if containsArgs(plans[0].Args, "-hwaccel") {
		t.Fatalf("expected software decode + hwupload path: %v", plans[0].Args)
	}
	if !containsFilter(plans[0].Args, "format=p010,hwupload") {
		t.Fatalf("expected 10-bit hwupload filter: %v", plans[0].Args)
	}
}

func TestPickHardwareEncodeFormat_NormalizesPreferredDisabledFormat(t *testing.T) {
	settings := db.DefaultTranscodingSettings()
	settings.EncodeFormats.H264 = false
	settings.EncodeFormats.HEVC = true
	settings.PreferredHardwareEncodeFormat = "h264"

	format := pickHardwareEncodeFormat(settings)

	if format != "hevc" {
		t.Fatalf("format = %q", format)
	}
}

func TestSoftwarePlan_IncludesCRFAndQualityFlags(t *testing.T) {
	settings := db.DefaultTranscodingSettings()
	settings.VAAPIEnabled = false
	settings.HardwareEncodingEnabled = false
	stream := videoStreamInfo{CodecName: "h264", PixelFmt: "yuv420p"}

	plans := buildTranscodePlans("/media/test.mkv", "/tmp/out.mp4", settings, stream, -1)

	sw := plans[0]
	if sw.Mode != "software" {
		t.Fatalf("expected software, got %q", sw.Mode)
	}

	// CRF should be present with default value.
	if !containsArgPair(sw.Args, "-crf", "22") {
		t.Fatalf("missing -crf 22 in args: %v", sw.Args)
	}

	// Audio bitrate should be present.
	if !containsArgPair(sw.Args, "-b:a", "192k") {
		t.Fatalf("missing -b:a 192k in args: %v", sw.Args)
	}

	// Stereo downmix should be present (default channels = 2).
	if !containsArgPair(sw.Args, "-ac", "2") {
		t.Fatalf("missing -ac 2 in args: %v", sw.Args)
	}

	// Keyframe interval should be present.
	if !containsArgPair(sw.Args, "-g", "48") {
		t.Fatalf("missing -g 48 in args: %v", sw.Args)
	}
	if !containsArgPair(sw.Args, "-keyint_min", "48") {
		t.Fatalf("missing -keyint_min 48 in args: %v", sw.Args)
	}
	if !containsArgPair(sw.Args, "-pix_fmt", "yuv420p") {
		t.Fatalf("missing browser-safe pixel format in args: %v", sw.Args)
	}
	if !containsArgPair(sw.Args, "-profile:v", "high") {
		t.Fatalf("missing h264 compatibility profile in args: %v", sw.Args)
	}
}

func TestHardwarePlan_IncludesQualityFlags(t *testing.T) {
	settings := db.DefaultTranscodingSettings()
	settings.VAAPIEnabled = true
	settings.HardwareEncodingEnabled = true
	stream := videoStreamInfo{CodecName: "h264", PixelFmt: "yuv420p"}

	plans := buildTranscodePlans("/media/test.mkv", "/tmp/out.mp4", settings, stream, -1)

	hw := plans[0]
	if hw.Mode != "hardware" {
		t.Fatalf("expected hardware, got %q", hw.Mode)
	}

	if !containsArgPair(hw.Args, "-b:a", "192k") {
		t.Fatalf("missing -b:a 192k in hardware args: %v", hw.Args)
	}
	if !containsArgPair(hw.Args, "-ac", "2") {
		t.Fatalf("missing -ac 2 in hardware args: %v", hw.Args)
	}
	if !containsArgPair(hw.Args, "-g", "48") {
		t.Fatalf("missing -g 48 in hardware args: %v", hw.Args)
	}
}

func TestSoftwarePlan_CustomSettings(t *testing.T) {
	settings := db.DefaultTranscodingSettings()
	settings.VAAPIEnabled = false
	settings.HardwareEncodingEnabled = false
	settings.CRF = 18
	settings.AudioBitrate = "320k"
	settings.AudioChannels = 0 // passthrough
	settings.Threads = 4
	settings.KeyframeInterval = 72
	settings.MaxBitrate = "10M"
	stream := videoStreamInfo{CodecName: "h264", PixelFmt: "yuv420p"}

	plans := buildTranscodePlans("/media/test.mkv", "/tmp/out.mp4", settings, stream, -1)

	sw := plans[0]
	if !containsArgPair(sw.Args, "-crf", "18") {
		t.Fatalf("expected crf 18: %v", sw.Args)
	}
	if !containsArgPair(sw.Args, "-b:a", "320k") {
		t.Fatalf("expected 320k audio bitrate: %v", sw.Args)
	}
	// audioChannels = 0 means passthrough, no -ac flag.
	if containsArgs(sw.Args, "-ac") {
		t.Fatalf("expected no -ac flag for passthrough: %v", sw.Args)
	}
	if !containsArgPair(sw.Args, "-threads", "4") {
		t.Fatalf("expected -threads 4: %v", sw.Args)
	}
	if !containsArgPair(sw.Args, "-g", "72") {
		t.Fatalf("expected -g 72: %v", sw.Args)
	}
	if !containsArgPair(sw.Args, "-maxrate", "10M") {
		t.Fatalf("expected -maxrate 10M: %v", sw.Args)
	}
	if !containsArgPair(sw.Args, "-bufsize", "10M") {
		t.Fatalf("expected -bufsize 10M: %v", sw.Args)
	}
}

func TestBuildTranscodePlans_AudioIndexMapping(t *testing.T) {
	settings := db.DefaultTranscodingSettings()
	settings.VAAPIEnabled = false
	settings.HardwareEncodingEnabled = false
	stream := videoStreamInfo{CodecName: "h264", PixelFmt: "yuv420p"}

	plans := buildTranscodePlans("/media/test.mkv", "/tmp/out.mp4", settings, stream, 3)

	sw := plans[0]
	if !containsArgPair(sw.Args, "-map", "0:3") {
		t.Fatalf("expected -map 0:3 for audio index 3: %v", sw.Args)
	}
}

func TestBuildHLSPlans_SoftwareOutputIsBrowserCompatible(t *testing.T) {
	settings := db.DefaultTranscodingSettings()
	settings.VAAPIEnabled = false
	settings.HardwareEncodingEnabled = false
	stream := videoStreamInfo{CodecName: "hevc", PixelFmt: "yuv420p10le"}

	plans := buildHLSPlans("/media/test.mkv", "/tmp/out", settings, stream, -1)

	if len(plans) != 1 {
		t.Fatalf("plan count = %d", len(plans))
	}
	if plans[0].Mode != "software" {
		t.Fatalf("mode = %q", plans[0].Mode)
	}
	if !containsArgPair(plans[0].Args, "-pix_fmt", "yuv420p") {
		t.Fatalf("missing browser-safe pixel format in HLS args: %v", plans[0].Args)
	}
	if !containsArgPair(plans[0].Args, "-profile:v", "high") {
		t.Fatalf("missing h264 compatibility profile in HLS args: %v", plans[0].Args)
	}
	if !containsArgPair(plans[0].Args, "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", hlsSegmentDurationSeconds)) {
		t.Fatalf("missing segment-aligned keyframe forcing in HLS args: %v", plans[0].Args)
	}
	if !containsArgPair(plans[0].Args, "-muxpreload", "0") || !containsArgPair(plans[0].Args, "-muxdelay", "0") {
		t.Fatalf("missing zero-delay HLS mux settings in args: %v", plans[0].Args)
	}
	if !containsArgPair(plans[0].Args, "-sc_threshold", "0") {
		t.Fatalf("missing scene-cut suppression in software HLS args: %v", plans[0].Args)
	}
}

func TestBuildHLSPlans_HardwareUsesSegmentAlignedKeyframes(t *testing.T) {
	settings := db.DefaultTranscodingSettings()
	settings.VAAPIEnabled = true
	settings.HardwareEncodingEnabled = true
	stream := videoStreamInfo{CodecName: "h264", PixelFmt: "yuv420p"}

	plans := buildHLSPlans("/media/test.mkv", "/tmp/out", settings, stream, -1)

	if len(plans) != 2 {
		t.Fatalf("plan count = %d", len(plans))
	}
	if plans[0].Mode != "hardware" {
		t.Fatalf("mode = %q", plans[0].Mode)
	}
	if !containsArgPair(plans[0].Args, "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", hlsSegmentDurationSeconds)) {
		t.Fatalf("missing segment-aligned keyframe forcing in hardware HLS args: %v", plans[0].Args)
	}
	if !containsArgPair(plans[0].Args, "-muxpreload", "0") || !containsArgPair(plans[0].Args, "-muxdelay", "0") {
		t.Fatalf("missing zero-delay HLS mux settings in hardware args: %v", plans[0].Args)
	}
}

func containsArgs(args []string, needle string) bool {
	for _, arg := range args {
		if arg == needle {
			return true
		}
	}
	return false
}

func containsFilter(args []string, needle string) bool {
	for _, arg := range args {
		if strings.Contains(arg, needle) {
			return true
		}
	}
	return false
}

func containsArgPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
