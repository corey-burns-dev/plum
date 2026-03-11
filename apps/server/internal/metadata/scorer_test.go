package metadata

import "testing"

func TestParsePathForTV_ExtractsShowYearAndStructure(t *testing.T) {
	info := ParsePathForTV("Show (2024)/Season 01/S01E01.mkv", "S01E01.mkv")
	if info.ShowName != "Show" {
		t.Fatalf("show name = %q", info.ShowName)
	}
	if info.Year != 2024 {
		t.Fatalf("year = %d", info.Year)
	}
	if !info.Structured {
		t.Fatal("expected structured path")
	}
}

func TestScoreTV_ShowSeasonLayoutClearsAutoMatchThreshold(t *testing.T) {
	info := tvInfoFromPath("Show/Season 01/S01E01.mkv", "S01E01.mkv")
	score := ScoreTV(&MatchResult{
		Title:       "Show",
		ReleaseDate: "2024-01-01",
		Provider:    "tmdb",
		ExternalID:  "10",
	}, info)
	if score < ScoreAutoMatch {
		t.Fatalf("score = %d, want >= %d", score, ScoreAutoMatch)
	}
}

func TestScoreTV_ShowDashSeasonLayoutClearsAutoMatchThreshold(t *testing.T) {
	info := tvInfoFromPath("Show-Season1/S01E01.mkv", "S01E01.mkv")
	score := ScoreTV(&MatchResult{
		Title:      "Show",
		Provider:   "tmdb",
		ExternalID: "10",
	}, info)
	if score < ScoreAutoMatch {
		t.Fatalf("score = %d, want >= %d", score, ScoreAutoMatch)
	}
}

func TestScoreTV_TrailingYearFolderStillMatchesSeriesTitle(t *testing.T) {
	info := tvInfoFromPath("Show (2024)/Season 01/S01E01.mkv", "S01E01.mkv")
	score := ScoreTV(&MatchResult{
		Title:       "Show",
		ReleaseDate: "2024-01-01",
		Provider:    "tmdb",
		ExternalID:  "10",
	}, info)
	if score < ScoreAutoMatch {
		t.Fatalf("score = %d, want >= %d", score, ScoreAutoMatch)
	}
}

func TestScoreTV_YearConflictDoesNotDisqualifyEpisodeMatch(t *testing.T) {
	info := tvInfoFromPath("Show (2024)/Season 01/S01E01.mkv", "S01E01.mkv")
	score := ScoreTV(&MatchResult{
		Title:       "Show",
		ReleaseDate: "2022-01-01",
		Provider:    "tmdb",
		ExternalID:  "10",
	}, info)
	if score < ScoreAutoMatch {
		t.Fatalf("score = %d, want >= %d", score, ScoreAutoMatch)
	}
}

func TestScoreMovie_ExactTitleAndYearClearsMovieThreshold(t *testing.T) {
	score := ScoreMovie(&MatchResult{
		Title:       "Die My Love",
		ReleaseDate: "2025-01-01",
		Provider:    "tmdb",
		ExternalID:  "10",
	}, MediaInfo{Title: "Die My Love", Year: 2025})
	if score < ScoreMovieAutoMatch {
		t.Fatalf("score = %d, want >= %d", score, ScoreMovieAutoMatch)
	}
}
