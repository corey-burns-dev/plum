package metadata

import (
	"regexp"
	"strconv"
	"strings"
)

// MediaInfo holds parsed hints from a media filename (title, season/episode, year, explicit IDs).
type MediaInfo struct {
	Title           string
	Season          int
	Episode         int
	EpisodeEnd      int
	AbsoluteEpisode int
	Year            int
	IsTV            bool
	StructuredTV    bool
	IsSpecial       bool
	PreferTVDB      bool
	TMDBID          int    // explicit from filename e.g. tmdbid-12345
	TVDBID          string // explicit from filename e.g. tvdbid-789
}

var (
	tvRegex1  = regexp.MustCompile(`(?i)(.*?)s(\d+)e(\d+)(?:\s*[-~]\s*e?(\d+))?`)
	tvRegex2  = regexp.MustCompile(`(?i)(.*?)(\d+)x(\d+)(?:\s*[-~]\s*(\d+))?`)
	yearRegex = regexp.MustCompile(`(19|20)\d{2}`)

	tmdbIDRegex = regexp.MustCompile(`(?i)tmdbid[-.]?(\d+)`)
	tvdbIDRegex = regexp.MustCompile(`(?i)tvdbid[-.]?(\d+)`)
	// release-group / quality noise to strip from title
	noiseRegex          = regexp.MustCompile(`(?i)\b(1080p|720p|480p|2160p|4k|web[-.]?dl|webrip|bluray|blu[-.]ray|bdrip|bd|remux|bdremux|x264|x265|hevc|h264|aac|dd5\.1|dd\s*5\s*1|ddp?\s*5\s*1|dts|flac|opus|10bit|multi[- ]?sub|dual[- ]?audio|uncensored)\b`)
	animeEpisodeRegex1  = regexp.MustCompile(`(?i)^(.*?)(?:\s+-\s+)(?:episode|ep|e)?\s*(\d{1,4})(?:\s*[-~]\s*(\d{1,4}))?(?:\b|$)`)
	animeEpisodeRegex2  = regexp.MustCompile(`(?i)^(.*?)(?:\s+)(?:episode|ep|e)\s*(\d{1,4})(?:\s*[-~]\s*(\d{1,4}))?(?:\b|$)`)
	animeEpisodeRegex3  = regexp.MustCompile(`(?i)^(.*?\b(?:ova|ona|special|specials))\s*(\d{1,4})(?:\s*[-~]\s*(\d{1,4}))?(?:\b|$)`)
	bracketedNoiseRegex = regexp.MustCompile(`\[[^\]]+\]|\{[^}]+\}`)
	specialRegex        = regexp.MustCompile(`(?i)\b(ova|ona|special|specials|ncop|nced)\b`)
	spaceRegex          = regexp.MustCompile(`\s+`)

	trailingYearParenRegex = regexp.MustCompile(`(?i)^(.*?)[\s._-]*\(((?:19|20)\d{2})\)\s*$`)
	trailingYearBareRegex  = regexp.MustCompile(`(?i)^(.*?)[\s._-]+((?:19|20)\d{2})\s*$`)
)

// NormalizeTitle lowercases, collapses spaces, strips common release/quality tags.
func NormalizeTitle(s string) string {
	s = strings.ToLower(s)
	s = noiseRegex.ReplaceAllString(s, " ")
	s = spaceRegex.ReplaceAllString(strings.TrimSpace(s), " ")
	return s
}

// NormalizeSeriesTitle removes a trailing year marker before normalizing a TV series title.
func NormalizeSeriesTitle(s string) string {
	title, _ := SplitTitleAndYear(s)
	return NormalizeTitle(title)
}

// SplitTitleAndYear separates a trailing year marker like "(2024)" or "2024" from a title.
func SplitTitleAndYear(s string) (string, int) {
	s = strings.TrimSpace(s)
	if m := trailingYearParenRegex.FindStringSubmatch(s); len(m) == 3 && strings.TrimSpace(m[1]) != "" {
		if year, err := strconv.Atoi(m[2]); err == nil {
			return strings.TrimSpace(m[1]), year
		}
	}
	if m := trailingYearBareRegex.FindStringSubmatch(s); len(m) == 3 && strings.TrimSpace(m[1]) != "" {
		if year, err := strconv.Atoi(m[2]); err == nil {
			return strings.TrimSpace(m[1]), year
		}
	}
	return s, 0
}

// ParseFilename extracts title, optional season/episode, year, and explicit provider IDs from a filename.
func ParseFilename(filename string) MediaInfo {
	orig := strings.TrimSpace(filename)
	base := strings.TrimSuffix(orig, filepathExt(orig))
	filename = strings.ReplaceAll(base, ".", " ")
	filename = strings.ReplaceAll(filename, "_", " ")

	info := MediaInfo{}

	// Explicit provider IDs
	if m := tmdbIDRegex.FindStringSubmatch(orig); len(m) == 2 {
		if id, err := strconv.Atoi(m[1]); err == nil {
			info.TMDBID = id
		}
	}
	if m := tvdbIDRegex.FindStringSubmatch(orig); len(m) == 2 {
		info.TVDBID = m[1]
	}

	// Year (last 4-digit year in range 1900-2099)
	if years := yearRegex.FindAllString(base, -1); len(years) > 0 {
		if y, err := strconv.Atoi(years[len(years)-1]); err == nil {
			info.Year = y
		}
	}

	if m := tvRegex1.FindStringSubmatch(filename); len(m) >= 4 {
		s, _ := strconv.Atoi(m[2])
		e, _ := strconv.Atoi(m[3])
		info.Title = normalizeStructuredSeriesTitle(m[1])
		info.Season = s
		info.Episode = e
		if len(m) > 4 && m[4] != "" {
			info.EpisodeEnd, _ = strconv.Atoi(m[4])
		}
		info.IsTV = true
		return info
	}

	if m := tvRegex2.FindStringSubmatch(filename); len(m) >= 4 {
		s, _ := strconv.Atoi(m[2])
		e, _ := strconv.Atoi(m[3])
		info.Title = normalizeStructuredSeriesTitle(m[1])
		info.Season = s
		info.Episode = e
		if len(m) > 4 && m[4] != "" {
			info.EpisodeEnd, _ = strconv.Atoi(m[4])
		}
		info.IsTV = true
		return info
	}

	if anime := parseAnimeLikeFilename(base); anime.Title != "" || anime.Episode > 0 {
		anime.TMDBID = info.TMDBID
		anime.TVDBID = info.TVDBID
		if anime.Year == 0 {
			anime.Year = info.Year
		}
		return anime
	}

	info.Title = NormalizeTitle(filename)
	info.IsTV = false
	return info
}

func parseAnimeLikeFilename(base string) MediaInfo {
	cleaned := cleanupReleaseName(base)
	if cleaned == "" {
		return MediaInfo{}
	}
	info := MediaInfo{}
	if specialRegex.MatchString(cleaned) {
		info.IsSpecial = true
	}
	for _, rx := range []*regexp.Regexp{animeEpisodeRegex1, animeEpisodeRegex2, animeEpisodeRegex3} {
		if m := rx.FindStringSubmatch(cleaned); len(m) == 4 {
			title := strings.TrimSpace(m[1])
			if !containsAlpha(title) {
				continue
			}
			ep, err := strconv.Atoi(m[2])
			if err != nil {
				continue
			}
			info.Title = NormalizeSeriesTitle(title)
			info.AbsoluteEpisode = ep
			if info.IsSpecial {
				info.Season = 0
				info.Episode = ep
			}
			if m[3] != "" {
				info.EpisodeEnd, _ = strconv.Atoi(m[3])
			}
			info.IsTV = true
			return info
		}
	}
	title, year := SplitTitleAndYear(cleaned)
	info.Title = NormalizeSeriesTitle(title)
	info.Year = year
	return info
}

func cleanupReleaseName(name string) string {
	name = strings.TrimSpace(strings.TrimSuffix(name, filepathExt(name)))
	name = bracketedNoiseRegex.ReplaceAllString(name, " ")
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, ".", " ")
	name = noiseRegex.ReplaceAllString(name, " ")
	name = spaceRegex.ReplaceAllString(strings.TrimSpace(name), " ")
	return name
}

func normalizeStructuredSeriesTitle(name string) string {
	name = cleanupReleaseName(name)
	name = strings.TrimSpace(strings.Trim(name, "-:"))
	return NormalizeSeriesTitle(name)
}

func containsAlpha(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func filepathExt(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[i:]
		}
		if name[i] == '/' || name[i] == '\\' {
			break
		}
	}
	return ""
}
