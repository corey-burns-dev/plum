package metadata

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type MovieInfo struct {
	Title      string
	Year       int
	Collection []string
	TMDBID     int
	TVDBID     string
	IsExtra    bool
}

var (
	discSegmentRegex    = regexp.MustCompile(`(?i)^(disc|cd)\s*\d+$`)
	extraSegmentRegex   = regexp.MustCompile(`(?i)^(extras?|featurettes?|samples?|trailers?|deleted scenes?|behind the scenes)$`)
	moviePrefixTagRegex = regexp.MustCompile(`^(?:\[[^\]]+\]\s*)+`)
	movieYearHintRegex  = regexp.MustCompile(`(?i)^(.*?)[\s._-]*\(?((?:19|20)\d{2})\)?(?:\b.*)?$`)
	genericMovieTitles  = map[string]struct{}{
		"movie": {}, "video": {}, "feature": {}, "main feature": {},
	}
)

func ParseMovie(relPath, filename string) MovieInfo {
	fileInfo := ParseFilename(filename)
	out := MovieInfo{
		TMDBID: fileInfo.TMDBID,
		TVDBID: fileInfo.TVDBID,
		Year:   fileInfo.Year,
	}

	relPath = filepath.ToSlash(strings.Trim(filepath.Clean(relPath), "/\\"))
	parts := strings.Split(relPath, "/")
	if len(parts) > 0 && strings.Contains(parts[len(parts)-1], ".") {
		parts = parts[:len(parts)-1]
	}

	var meaningful []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if extraSegmentRegex.MatchString(part) {
			out.IsExtra = true
			continue
		}
		if discSegmentRegex.MatchString(part) {
			continue
		}
		meaningful = append(meaningful, part)
	}

	baseTitle, baseYear := parseMovieTitleCandidate(strings.TrimSpace(strings.TrimSuffix(filename, filepath.Ext(filename))))
	if baseYear > 0 && out.Year == 0 {
		out.Year = baseYear
	}

	if len(meaningful) > 0 {
		title, year := SplitTitleAndYear(meaningful[len(meaningful)-1])
		if title != "" {
			out.Title = title
		}
		if year > 0 {
			out.Year = year
		}
		if len(meaningful) > 1 {
			out.Collection = append(out.Collection, meaningful[:len(meaningful)-1]...)
		}
	}

	if out.Title == "" || (isGenericMovieTitle(out.Title) && len(meaningful) == 0) {
		out.Title = strings.TrimSpace(baseTitle)
	}
	if extraSegmentRegex.MatchString(strings.TrimSpace(baseTitle)) || strings.Contains(strings.ToLower(baseTitle), "sample") {
		out.IsExtra = true
	}
	if out.Title == "" {
		out.Title = strings.TrimSpace(strings.TrimSuffix(filename, filepath.Ext(filename)))
	}

	return out
}

func parseMovieTitleCandidate(name string) (string, int) {
	cleaned := cleanupReleaseName(name)
	cleaned = moviePrefixTagRegex.ReplaceAllString(cleaned, "")
	cleaned = strings.TrimSpace(strings.Trim(cleaned, "-:"))
	if m := movieYearHintRegex.FindStringSubmatch(cleaned); len(m) == 3 {
		title := strings.TrimSpace(strings.Trim(m[1], "-:"))
		if title != "" {
			year, _ := strconv.Atoi(m[2])
			return title, year
		}
	}
	return SplitTitleAndYear(cleaned)
}

func MovieMediaInfo(info MovieInfo) MediaInfo {
	return MediaInfo{
		Title:  NormalizeTitle(info.Title),
		Year:   info.Year,
		TMDBID: info.TMDBID,
		TVDBID: info.TVDBID,
	}
}

func MovieDisplayTitle(info MovieInfo, fallback string) string {
	if info.Title != "" {
		return info.Title
	}
	return fallback
}

func isGenericMovieTitle(title string) bool {
	if title == "" {
		return true
	}
	_, ok := genericMovieTitles[NormalizeTitle(title)]
	return ok
}
