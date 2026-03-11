package metadata

import (
	"strconv"
	"strings"
)

// Confidence thresholds and margin from library-ident2.md
const (
	ScoreAutoMatch      = 700
	ScoreMovieAutoMatch = 560
	ScoreTentative      = 500
	ScoreMargin         = 40
	ScoreExplicitID     = 1000
	ScoreExactTitle     = 400
	ScoreTVSeasonHint   = 220
	ScoreTVEpisodeHint  = 260
	ScoreTVPathHint     = 80
	ScoreYearMatch      = 180
	ScoreYearConflict   = -300
	ScoreTitleConflict  = -250
	ScoreTVDBPreferred  = 60
)

// ScoreTV returns a score for a TV candidate against parsed MediaInfo.
// Higher is better. Explicit ID match wins; title/year help; conflicts reduce score.
func ScoreTV(candidate *MatchResult, info MediaInfo) int {
	if candidate == nil {
		return 0
	}
	score := 0
	// Explicit provider ID match
	if info.TMDBID > 0 && candidate.Provider == "tmdb" && candidate.ExternalID == strconv.Itoa(info.TMDBID) {
		score += ScoreExplicitID
	}
	if info.TVDBID != "" && candidate.Provider == "tvdb" && candidate.ExternalID == info.TVDBID {
		score += ScoreExplicitID
	}
	// Title: normalized exact match (candidate title may be "Show Name" or "Show - S01E02 - Episode")
	candTitle := NormalizeSeriesTitle(extractSeriesTitle(candidate.Title))
	infoNorm := NormalizeSeriesTitle(info.Title)
	if candTitle != "" && infoNorm != "" && candTitle == infoNorm {
		score += ScoreExactTitle
	} else if candTitle != "" && infoNorm != "" && !strings.Contains(candTitle, infoNorm) && !strings.Contains(infoNorm, candTitle) {
		score += ScoreTitleConflict
	}
	if info.Season > 0 {
		score += ScoreTVSeasonHint
	}
	if info.Episode > 0 {
		score += ScoreTVEpisodeHint
	}
	if info.StructuredTV {
		score += ScoreTVPathHint
	}
	if info.PreferTVDB && candidate.Provider == "tvdb" {
		score += ScoreTVDBPreferred
	}
	// Year
	candYear := parseYear(candidate.ReleaseDate)
	if info.Year > 0 && candYear > 0 {
		if candYear == info.Year {
			score += ScoreYearMatch
		} else if info.Season == 0 && info.Episode == 0 {
			score += ScoreYearConflict
		}
	}
	return score
}

// ScoreMovie returns a score for a movie candidate against parsed MediaInfo.
func ScoreMovie(candidate *MatchResult, info MediaInfo) int {
	if candidate == nil {
		return 0
	}
	score := 0
	if info.TMDBID > 0 && candidate.Provider == "tmdb" && candidate.ExternalID == strconv.Itoa(info.TMDBID) {
		score += ScoreExplicitID
	}
	if info.TVDBID != "" && candidate.Provider == "tvdb" && candidate.ExternalID == info.TVDBID {
		score += ScoreExplicitID
	}
	candTitle := NormalizeTitle(candidate.Title)
	infoNorm := NormalizeTitle(info.Title)
	if candTitle != "" && infoNorm != "" && candTitle == infoNorm {
		score += ScoreExactTitle
	} else if candTitle != "" && infoNorm != "" && !strings.Contains(candTitle, infoNorm) && !strings.Contains(infoNorm, candTitle) {
		score += ScoreTitleConflict
	}
	candYear := parseYear(candidate.ReleaseDate)
	if info.Year > 0 && candYear > 0 {
		if candYear == info.Year {
			score += ScoreYearMatch
		} else {
			score += ScoreYearConflict
		}
	}
	return score
}

func extractSeriesTitle(s string) string {
	// "Show Name - S01E02 - Episode" -> "Show Name"
	if i := strings.Index(strings.ToLower(s), " - s"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	if i := strings.Index(s, " - "); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func parseYear(s string) int {
	if len(s) < 4 {
		return 0
	}
	for i := 0; i <= len(s)-4; i++ {
		if s[i] >= '1' && s[i] <= '2' && s[i+1] >= '0' && s[i+1] <= '9' {
			y, _ := strconv.Atoi(s[i : i+4])
			if y >= 1900 && y <= 2099 {
				return y
			}
		}
	}
	return 0
}
