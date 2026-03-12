package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	appSettingsKeyTranscoding = "transcoding"
	defaultVAAPIDevicePath    = "/dev/dri/renderD128"
)

var (
	ErrNoHardwareEncodeFormats = errors.New("at least one hardware encode format must be enabled")
	ErrInvalidPreferredFormat  = errors.New("preferred hardware encode format is invalid")
)

type DecodeCodecSettings struct {
	H264      bool `json:"h264"`
	HEVC      bool `json:"hevc"`
	MPEG2     bool `json:"mpeg2"`
	VC1       bool `json:"vc1"`
	VP8       bool `json:"vp8"`
	VP9       bool `json:"vp9"`
	AV1       bool `json:"av1"`
	HEVC10Bit bool `json:"hevc10bit"`
	VP910Bit  bool `json:"vp910bit"`
}

func (s DecodeCodecSettings) Enabled(codec string) bool {
	switch codec {
	case "h264":
		return s.H264
	case "hevc":
		return s.HEVC
	case "mpeg2":
		return s.MPEG2
	case "vc1":
		return s.VC1
	case "vp8":
		return s.VP8
	case "vp9":
		return s.VP9
	case "av1":
		return s.AV1
	case "hevc10bit":
		return s.HEVC10Bit
	case "vp910bit":
		return s.VP910Bit
	default:
		return false
	}
}

type EncodeFormatSettings struct {
	H264 bool `json:"h264"`
	HEVC bool `json:"hevc"`
	AV1  bool `json:"av1"`
}

func (s EncodeFormatSettings) Enabled(format string) bool {
	switch format {
	case "h264":
		return s.H264
	case "hevc":
		return s.HEVC
	case "av1":
		return s.AV1
	default:
		return false
	}
}

func (s EncodeFormatSettings) AnyEnabled() bool {
	return s.H264 || s.HEVC || s.AV1
}

type TranscodingSettings struct {
	VAAPIEnabled                  bool                 `json:"vaapiEnabled"`
	VAAPIDevicePath               string               `json:"vaapiDevicePath"`
	DecodeCodecs                  DecodeCodecSettings  `json:"decodeCodecs"`
	HardwareEncodingEnabled       bool                 `json:"hardwareEncodingEnabled"`
	EncodeFormats                 EncodeFormatSettings `json:"encodeFormats"`
	PreferredHardwareEncodeFormat string               `json:"preferredHardwareEncodeFormat"`
	AllowSoftwareFallback         bool                 `json:"allowSoftwareFallback"`
	CRF                           int                  `json:"crf"`
	AudioBitrate                  string               `json:"audioBitrate"`
	AudioChannels                 int                  `json:"audioChannels"`
	Threads                       int                  `json:"threads"`
	KeyframeInterval              int                  `json:"keyframeInterval"`
	MaxBitrate                    string               `json:"maxBitrate"`
}

type TranscodingSettingsWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func DefaultTranscodingSettings() TranscodingSettings {
	return TranscodingSettings{
		VAAPIEnabled:    true,
		VAAPIDevicePath: defaultVAAPIDevicePath,
		DecodeCodecs: DecodeCodecSettings{
			H264:      true,
			HEVC:      true,
			MPEG2:     true,
			VC1:       true,
			VP8:       true,
			VP9:       true,
			AV1:       true,
			HEVC10Bit: true,
			VP910Bit:  true,
		},
		HardwareEncodingEnabled: true,
		EncodeFormats: EncodeFormatSettings{
			H264: true,
			HEVC: false,
			AV1:  false,
		},
		PreferredHardwareEncodeFormat: "h264",
		AllowSoftwareFallback:         true,
		CRF:                           22,
		AudioBitrate:                  "192k",
		AudioChannels:                 2,
		Threads:                       0,
		KeyframeInterval:              48,
		MaxBitrate:                    "",
	}
}

func NormalizeTranscodingSettings(settings TranscodingSettings) TranscodingSettings {
	defaults := DefaultTranscodingSettings()
	settings.VAAPIDevicePath = strings.TrimSpace(settings.VAAPIDevicePath)
	if settings.VAAPIDevicePath == "" {
		settings.VAAPIDevicePath = defaults.VAAPIDevicePath
	}
	if settings.PreferredHardwareEncodeFormat == "" {
		settings.PreferredHardwareEncodeFormat = defaults.PreferredHardwareEncodeFormat
	}
	if !settings.EncodeFormats.Enabled(settings.PreferredHardwareEncodeFormat) {
		switch {
		case settings.EncodeFormats.H264:
			settings.PreferredHardwareEncodeFormat = "h264"
		case settings.EncodeFormats.HEVC:
			settings.PreferredHardwareEncodeFormat = "hevc"
		case settings.EncodeFormats.AV1:
			settings.PreferredHardwareEncodeFormat = "av1"
		default:
			settings.PreferredHardwareEncodeFormat = defaults.PreferredHardwareEncodeFormat
		}
	}
	if settings.CRF <= 0 || settings.CRF > 51 {
		settings.CRF = defaults.CRF
	}
	settings.AudioBitrate = strings.TrimSpace(settings.AudioBitrate)
	if settings.AudioBitrate == "" {
		settings.AudioBitrate = defaults.AudioBitrate
	}
	if settings.AudioChannels < 0 {
		settings.AudioChannels = defaults.AudioChannels
	}
	if settings.Threads < 0 {
		settings.Threads = defaults.Threads
	}
	if settings.KeyframeInterval <= 0 {
		settings.KeyframeInterval = defaults.KeyframeInterval
	}
	settings.MaxBitrate = strings.TrimSpace(settings.MaxBitrate)
	return settings
}

func ValidateTranscodingSettings(settings TranscodingSettings) error {
	settings = NormalizeTranscodingSettings(settings)
	if settings.HardwareEncodingEnabled && !settings.EncodeFormats.AnyEnabled() {
		return ErrNoHardwareEncodeFormats
	}
	if settings.PreferredHardwareEncodeFormat != "h264" &&
		settings.PreferredHardwareEncodeFormat != "hevc" &&
		settings.PreferredHardwareEncodeFormat != "av1" {
		return ErrInvalidPreferredFormat
	}
	return nil
}

func GetTranscodingSettings(dbConn *sql.DB) (TranscodingSettings, error) {
	var raw string
	err := dbConn.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, appSettingsKeyTranscoding).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultTranscodingSettings(), nil
	}
	if err != nil {
		return TranscodingSettings{}, err
	}

	settings := DefaultTranscodingSettings()
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return TranscodingSettings{}, err
	}
	return NormalizeTranscodingSettings(settings), nil
}

func SaveTranscodingSettings(dbConn *sql.DB, settings TranscodingSettings) (TranscodingSettings, error) {
	settings = NormalizeTranscodingSettings(settings)
	if err := ValidateTranscodingSettings(settings); err != nil {
		return TranscodingSettings{}, err
	}

	raw, err := json.Marshal(settings)
	if err != nil {
		return TranscodingSettings{}, err
	}

	now := time.Now().UTC()
	if _, err := dbConn.Exec(
		`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		appSettingsKeyTranscoding,
		string(raw),
		now,
	); err != nil {
		return TranscodingSettings{}, err
	}

	return settings, nil
}
