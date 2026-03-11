package transcoder

import (
	"strings"
	"testing"

	"plum/internal/db"
)

func TestBuildTranscodePlans_SoftwareOnlyWhenHardwareDisabled(t *testing.T) {
	settings := db.DefaultTranscodingSettings()
	stream := videoStreamInfo{CodecName: "h264", PixelFmt: "yuv420p"}

	plans := buildTranscodePlans("/media/test.mkv", "/tmp/out.mp4", settings, stream)

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

	plans := buildTranscodePlans("/media/test.mkv", "/tmp/out.mp4", settings, stream)

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

	plans := buildTranscodePlans("/media/test.mkv", "/tmp/out.mp4", settings, stream)

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
