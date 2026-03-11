package metadata

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// PathInfo holds show name, season, and episode derived from directory structure.
type PathInfo struct {
	ShowName   string
	Season     int
	Episode    int
	EpisodeEnd int
	IsSpecial  bool
	Year       int
	Structured bool
}

var (
	// Season folder: "Season 01", "Season 1", "S1", "s01", "season 1", "season1"
	seasonFolderRegex  = regexp.MustCompile(`(?i)^(?:season\s*)?(\d+)(?:\s*\[[^\]]+\])?$`)
	seasonSRegex       = regexp.MustCompile(`(?i)^s(\d+)(?:\s*\[[^\]]+\])?$`)
	specialFolderRegex = regexp.MustCompile(`(?i)^(specials|ova|ona)$`)
	// "ShowName-Season1", "ShowName - Season 1", "ShowName Season 1"
	showSeasonRegex  = regexp.MustCompile(`(?i)^(.+?)\s*[-–—]\s*season\s*(\d+)$`)
	showSeason2Regex = regexp.MustCompile(`(?i)^(.+?)\s+season\s*(\d+)$`)
	// Episode folder: "Episode 01", "Episode 1", "Ep 1", "Ep01", "E01"
	episodeFolderRegex = regexp.MustCompile(`(?i)^(?:episode|ep)\s*(\d+)$`)
	episodeERegex      = regexp.MustCompile(`(?i)^e(\d+)$`)
	episodeRangeRegex  = regexp.MustCompile(`(?i)^(?:episode|ep|e)?\s*(\d+)\s*[-~]\s*(\d+)$`)
	// Filename episode when no SxxExx: "episode 1", "ep 1", "ep01", "e01", or leading number
	episodeOnlyRegex = regexp.MustCompile(`(?i)(?:episode|ep)\s*(\d+)`)
	episodeOnlyE     = regexp.MustCompile(`(?i)e(\d+)`)
	episodeOnlyRange = regexp.MustCompile(`(?i)(?:episode|ep|e)?\s*(\d+)\s*[-~]\s*(\d+)`)
	leadingNumRegex  = regexp.MustCompile(`^(\d+)`)
	discFolderRegex  = regexp.MustCompile(`(?i)^(?:disc|cd)\s*(\d+)$`)
)

// ParsePathForTV extracts show name, season, and episode from a path relative to the library root.
// Handles: /anime/Dragonball/Season 01/episode1.mkv, /ShowName-Season1/S01E01.mkv, /Show/Season 1/Episode 2/video.mkv.
func ParsePathForTV(relPath, filename string) PathInfo {
	out := PathInfo{}
	relPath = filepath.ToSlash(strings.Trim(filepath.Clean(relPath), "/\\"))
	if relPath == "" {
		return out
	}
	parts := strings.Split(relPath, "/")
	// Drop the filename if it was included (last segment containing a dot is the file)
	if len(parts) > 0 && strings.Contains(parts[len(parts)-1], ".") {
		parts = parts[:len(parts)-1]
	}
	var firstDir string
	for i, seg := range parts {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if firstDir == "" {
			firstDir = seg
		}
		// Check for "ShowName-Season1" or "ShowName - Season 1"
		if m := showSeasonRegex.FindStringSubmatch(seg); len(m) == 3 {
			out.ShowName, out.Year = SplitTitleAndYear(strings.TrimSpace(m[1]))
			if s, err := strconv.Atoi(m[2]); err == nil {
				out.Season = s
			}
			out.Structured = true
			continue
		}
		if m := showSeason2Regex.FindStringSubmatch(seg); len(m) == 3 {
			out.ShowName, out.Year = SplitTitleAndYear(strings.TrimSpace(m[1]))
			if s, err := strconv.Atoi(m[2]); err == nil {
				out.Season = s
			}
			out.Structured = true
			continue
		}
		// "Season 01", "S1", "s01"
		if m := seasonFolderRegex.FindStringSubmatch(seg); len(m) == 2 {
			if s, err := strconv.Atoi(m[1]); err == nil {
				out.Season = s
			}
			out.Structured = true
			continue
		}
		if m := seasonSRegex.FindStringSubmatch(seg); len(m) == 2 {
			if s, err := strconv.Atoi(m[1]); err == nil {
				out.Season = s
			}
			out.Structured = true
			continue
		}
		if specialFolderRegex.MatchString(seg) {
			out.Season = 0
			out.IsSpecial = true
			out.Structured = true
			continue
		}
		// "Episode 01", "Ep 1", "E01"
		if m := episodeRangeRegex.FindStringSubmatch(seg); len(m) == 3 {
			if e, err := strconv.Atoi(m[1]); err == nil {
				out.Episode = e
			}
			if e, err := strconv.Atoi(m[2]); err == nil {
				out.EpisodeEnd = e
			}
			out.Structured = true
			continue
		}
		if m := episodeFolderRegex.FindStringSubmatch(seg); len(m) == 2 {
			if e, err := strconv.Atoi(m[1]); err == nil {
				out.Episode = e
			}
			out.Structured = true
			continue
		}
		if m := episodeERegex.FindStringSubmatch(seg); len(m) == 2 {
			if e, err := strconv.Atoi(m[1]); err == nil {
				out.Episode = e
			}
			out.Structured = true
			continue
		}
		// If we don't have a show name yet and this isn't a season/episode folder, use as show (e.g. "Dragonball")
		if out.ShowName == "" && out.Season == 0 && out.Episode == 0 && i == 0 {
			out.ShowName, out.Year = SplitTitleAndYear(seg)
		}
	}
	if out.ShowName == "" && firstDir != "" {
		out.ShowName, out.Year = SplitTitleAndYear(firstDir)
	}
	// Episode from filename when not in path and filename has no SxxExx
	if out.Episode == 0 {
		start, end := episodeFromFilename(filename)
		out.Episode = start
		out.EpisodeEnd = end
		if out.Episode > 0 {
			out.Structured = true
		}
	}
	return out
}

func episodeFromFilename(filename string) (int, int) {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	base = strings.ReplaceAll(base, ".", " ")
	base = strings.ReplaceAll(base, "_", " ")
	if m := episodeOnlyRange.FindStringSubmatch(base); len(m) == 3 {
		start, err1 := strconv.Atoi(m[1])
		end, err2 := strconv.Atoi(m[2])
		if err1 == nil && err2 == nil {
			return start, end
		}
	}
	if m := episodeOnlyRegex.FindStringSubmatch(base); len(m) == 2 {
		if e, err := strconv.Atoi(m[1]); err == nil {
			return e, 0
		}
	}
	if m := episodeOnlyE.FindStringSubmatch(base); len(m) == 2 {
		if e, err := strconv.Atoi(m[1]); err == nil {
			return e, 0
		}
	}
	if m := leadingNumRegex.FindStringSubmatch(strings.TrimSpace(base)); len(m) == 2 {
		if e, err := strconv.Atoi(m[1]); err == nil && e < 1000 {
			return e, 0
		}
	}
	return 0, 0
}

// MergePathInfo merges path-derived PathInfo with filename-derived MediaInfo for TV/anime.
// Filename SxxExx takes precedence for season/episode when present; path fills in otherwise.
// Title for search: use path show name when filename has no clear show (e.g. "episode1").
func MergePathInfo(pathInfo PathInfo, fileInfo MediaInfo) MediaInfo {
	out := fileInfo
	if fileInfo.Season == 0 && pathInfo.Season > 0 {
		out.Season = pathInfo.Season
	}
	if fileInfo.Episode == 0 && pathInfo.Episode > 0 {
		out.Episode = pathInfo.Episode
	}
	if fileInfo.EpisodeEnd == 0 && pathInfo.EpisodeEnd > 0 {
		out.EpisodeEnd = pathInfo.EpisodeEnd
	}
	if fileInfo.Year == 0 && pathInfo.Year > 0 {
		out.Year = pathInfo.Year
	}
	if !fileInfo.IsSpecial && pathInfo.IsSpecial {
		out.IsSpecial = true
		if out.Season == 0 {
			out.Season = 0
		}
	}
	if pathInfo.Structured {
		out.StructuredTV = true
		out.IsTV = true
	}
	// Use path show name for search when we have a folder-derived show and filename looks like a single episode
	if pathInfo.ShowName != "" && (fileInfo.Title == "" || IsGenericEpisodeTitle(fileInfo.Title, fileInfo.Season, fileInfo.Episode)) {
		out.Title = NormalizeSeriesTitle(pathInfo.ShowName)
		out.IsTV = true
	}
	return out
}

// IsGenericEpisodeTitle reports whether the title looks like a generic episode label (e.g. "episode 1") rather than a show name.
func IsGenericEpisodeTitle(title string, season, episode int) bool {
	norm := strings.TrimSpace(strings.ToLower(title))
	if norm == "" {
		return true
	}
	// "episode 1", "ep 1", "ep01", "1" etc.
	if episodeOnlyRegex.MatchString(norm) || episodeOnlyE.MatchString(norm) {
		return true
	}
	if leadingNumRegex.MatchString(norm) && len(norm) <= 4 {
		return true
	}
	return false
}

// MusicPathInfo holds artist and album derived from directory structure (e.g. Led Zeppelin/Album1/track.mp3).
type MusicPathInfo struct {
	Artist     string
	Album      string
	DiscNumber int
}

// ParsePathForMusic extracts artist and album from a path relative to the library root.
// Handles: /Led Zeppelin/IV/track.mp3 (artist=Led Zeppelin, album=IV), /Artist/track.mp3 (album empty).
func ParsePathForMusic(relPath, filename string) MusicPathInfo {
	out := MusicPathInfo{}
	relPath = filepath.ToSlash(strings.Trim(filepath.Clean(relPath), "/\\"))
	if relPath == "" {
		return out
	}
	parts := strings.Split(relPath, "/")
	if len(parts) > 0 && strings.Contains(parts[len(parts)-1], ".") {
		parts = parts[:len(parts)-1]
	}
	for _, seg := range parts {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if m := discFolderRegex.FindStringSubmatch(seg); len(m) == 2 {
			if disc, err := strconv.Atoi(m[1]); err == nil {
				out.DiscNumber = disc
			}
			continue
		}
		if out.Artist == "" {
			out.Artist = seg
			continue
		}
		if out.Album == "" {
			out.Album = seg
			continue
		}
	}
	return out
}

// MusicDisplayTitle builds a display title from path info and track filename (e.g. "Artist - Album - Track").
func MusicDisplayTitle(pathInfo MusicPathInfo, trackTitle string) string {
	if pathInfo.Artist == "" {
		return trackTitle
	}
	if pathInfo.Album == "" {
		return pathInfo.Artist + " - " + trackTitle
	}
	return pathInfo.Artist + " - " + pathInfo.Album + " - " + trackTitle
}

// PathSegmentsForMovie returns directory segments from relPath (folder names only, no file).
// Used for collection-style layouts like "Star Wars Collection/Star Wars 1/movie.mkv".
func PathSegmentsForMovie(relPath, filename string) []string {
	relPath = filepath.ToSlash(strings.Trim(filepath.Clean(relPath), "/\\"))
	if relPath == "" {
		return nil
	}
	parts := strings.Split(relPath, "/")
	if len(parts) > 0 && strings.Contains(parts[len(parts)-1], ".") {
		parts = parts[:len(parts)-1]
	}
	var out []string
	for _, seg := range parts {
		seg = strings.TrimSpace(seg)
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}
