package metadata

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type MusicMetadata struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	DiscNumber  int
	TrackNumber int
	ReleaseYear int
}

func ReadAudioMetadata(ctx context.Context, path string) (MusicMetadata, int, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration:format_tags",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return MusicMetadata{}, 0, err
	}

	var parsed struct {
		Format struct {
			Duration string            `json:"duration"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return MusicMetadata{}, 0, err
	}

	meta := MusicMetadata{
		Title:       firstNonEmptyTag(parsed.Format.Tags, "title"),
		Artist:      firstNonEmptyTag(parsed.Format.Tags, "artist"),
		Album:       firstNonEmptyTag(parsed.Format.Tags, "album"),
		AlbumArtist: firstNonEmptyTag(parsed.Format.Tags, "album_artist", "albumartist"),
		ReleaseYear: parseTagInt(firstNonEmptyTag(parsed.Format.Tags, "date", "year")),
		DiscNumber:  parseTagInt(firstNonEmptyTag(parsed.Format.Tags, "disc", "discnumber")),
		TrackNumber: parseTagInt(firstNonEmptyTag(parsed.Format.Tags, "track", "tracknumber")),
	}

	duration := 0
	if parsed.Format.Duration != "" {
		if f, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
			duration = int(f)
		}
	}
	return meta, duration, nil
}

func MergeMusicMetadata(pathInfo MusicPathInfo, tag MusicMetadata, fallbackTitle string) MusicMetadata {
	out := tag
	if out.Title == "" {
		out.Title = fallbackTitle
	}
	if out.Artist == "" {
		out.Artist = pathInfo.Artist
	}
	if out.Album == "" {
		out.Album = pathInfo.Album
	}
	if out.DiscNumber == 0 {
		out.DiscNumber = pathInfo.DiscNumber
	}
	return out
}

func firstNonEmptyTag(tags map[string]string, keys ...string) string {
	for _, key := range keys {
		for actualKey, value := range tags {
			if strings.EqualFold(actualKey, key) && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func parseTagInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
