package metadata

import (
	"context"
	"testing"
)

func tvInfoFromPath(relPath, filename string) MediaInfo {
	fileInfo := ParseFilename(filename)
	pathInfo := ParsePathForTV(relPath, filename)
	return MergePathInfo(pathInfo, fileInfo)
}

type stubTVProvider struct {
	name          string
	searchResults []MatchResult
	episodeResult *MatchResult
	episodeCalls  []string
}

func (s *stubTVProvider) ProviderName() string {
	return s.name
}

func (s *stubTVProvider) SearchTV(_ context.Context, _ string) ([]MatchResult, error) {
	return s.searchResults, nil
}

func (s *stubTVProvider) GetEpisode(_ context.Context, seriesID string, season, episode int) (*MatchResult, error) {
	s.episodeCalls = append(s.episodeCalls, seriesID)
	if s.episodeResult == nil {
		return nil, nil
	}
	return s.episodeResult, nil
}

type stubMovieProvider struct {
	searchResults []MatchResult
	lookupResult  *MatchResult
	lookupCalls   []string
}

func (s *stubMovieProvider) SearchMovie(_ context.Context, _ string) ([]MatchResult, error) {
	return s.searchResults, nil
}

func (s *stubMovieProvider) GetMovie(_ context.Context, movieID string) (*MatchResult, error) {
	s.lookupCalls = append(s.lookupCalls, movieID)
	if s.lookupResult == nil {
		return nil, nil
	}
	return s.lookupResult, nil
}

type stubSeriesDetailsProvider struct {
	details *SeriesDetails
}

func (s *stubSeriesDetailsProvider) GetSeriesDetails(_ context.Context, _ int) (*SeriesDetails, error) {
	return s.details, nil
}

func TestIdentifyTV_ExplicitTMDBIDUsesEpisodeMetadata(t *testing.T) {
	tmdb := &stubTVProvider{
		name: "tmdb",
		episodeResult: &MatchResult{
			Title:      "Show - S01E02 - Episode",
			Provider:   "tmdb",
			ExternalID: "123",
		},
	}
	p := &Pipeline{
		tvProviders:           []TVProvider{tmdb},
		seriesDetailsProvider: &stubSeriesDetailsProvider{details: &SeriesDetails{Name: "Show"}},
	}

	res := p.IdentifyTV(context.Background(), MediaInfo{TMDBID: 123, Season: 1, Episode: 2})
	if res == nil {
		t.Fatal("expected match")
	}
	if res.Title != "Show - S01E02 - Episode" {
		t.Fatalf("title = %q", res.Title)
	}
	if len(tmdb.episodeCalls) != 1 || tmdb.episodeCalls[0] != "123" {
		t.Fatalf("episode lookup calls = %#v", tmdb.episodeCalls)
	}
}

func TestIdentifyTV_DoesNotFallbackToFirstLowConfidenceResult(t *testing.T) {
	tmdb := &stubTVProvider{
		name: "tmdb",
		searchResults: []MatchResult{
			{Title: "Completely Different Show", Provider: "tmdb", ExternalID: "10"},
		},
	}
	p := &Pipeline{tvProviders: []TVProvider{tmdb}}

	res := p.IdentifyTV(context.Background(), MediaInfo{Title: "Wanted Show"})
	if res != nil {
		t.Fatalf("expected no match, got %#v", res)
	}
}

func TestIdentifyTV_AutoMatchesShowSeasonLayout(t *testing.T) {
	tmdb := &stubTVProvider{
		name: "tmdb",
		searchResults: []MatchResult{
			{Title: "Show", ReleaseDate: "2024-01-01", Provider: "tmdb", ExternalID: "10"},
		},
		episodeResult: &MatchResult{
			Title:      "Show - S01E01 - Pilot",
			Provider:   "tmdb",
			ExternalID: "10",
		},
	}
	p := &Pipeline{tvProviders: []TVProvider{tmdb}}

	info := tvInfoFromPath("Show/Season 01/S01E01.mkv", "S01E01.mkv")
	res := p.IdentifyTV(context.Background(), info)
	if res == nil {
		t.Fatal("expected match")
	}
	if res.Title != "Show - S01E01 - Pilot" {
		t.Fatalf("title = %q", res.Title)
	}
}

func TestIdentifyTV_AutoMatchesShowDashSeasonLayout(t *testing.T) {
	tmdb := &stubTVProvider{
		name: "tmdb",
		searchResults: []MatchResult{
			{Title: "Show", Provider: "tmdb", ExternalID: "10"},
		},
		episodeResult: &MatchResult{
			Title:      "Show - S01E01 - Pilot",
			Provider:   "tmdb",
			ExternalID: "10",
		},
	}
	p := &Pipeline{tvProviders: []TVProvider{tmdb}}

	info := tvInfoFromPath("Show-Season1/S01E01.mkv", "S01E01.mkv")
	res := p.IdentifyTV(context.Background(), info)
	if res == nil {
		t.Fatal("expected match")
	}
	if res.Title != "Show - S01E01 - Pilot" {
		t.Fatalf("title = %q", res.Title)
	}
}

func TestIdentifyTV_StripsTrailingYearFromShowFolderTitle(t *testing.T) {
	tmdb := &stubTVProvider{
		name: "tmdb",
		searchResults: []MatchResult{
			{Title: "Show", ReleaseDate: "2024-01-01", Provider: "tmdb", ExternalID: "10"},
		},
		episodeResult: &MatchResult{
			Title:      "Show - S01E01 - Pilot",
			Provider:   "tmdb",
			ExternalID: "10",
		},
	}
	p := &Pipeline{tvProviders: []TVProvider{tmdb}}

	info := tvInfoFromPath("Show (2024)/Season 01/S01E01.mkv", "S01E01.mkv")
	res := p.IdentifyTV(context.Background(), info)
	if res == nil {
		t.Fatal("expected match")
	}
	if res.Title != "Show - S01E01 - Pilot" {
		t.Fatalf("title = %q", res.Title)
	}
}

func TestIdentifyTV_LeavesAmbiguousCandidatesUnmatched(t *testing.T) {
	tmdb := &stubTVProvider{
		name: "tmdb",
		searchResults: []MatchResult{
			{Title: "Show", ReleaseDate: "2024-01-01", Provider: "tmdb", ExternalID: "10"},
			{Title: "Show", ReleaseDate: "2024-01-01", Provider: "tmdb", ExternalID: "11"},
		},
	}
	p := &Pipeline{tvProviders: []TVProvider{tmdb}}

	info := tvInfoFromPath("Show/Season 01/S01E01.mkv", "S01E01.mkv")
	res := p.IdentifyTV(context.Background(), info)
	if res != nil {
		t.Fatalf("expected no match, got %#v", res)
	}
}

func TestIdentifyTV_UsesMatchingProviderForEpisodeLookup(t *testing.T) {
	tmdb := &stubTVProvider{name: "tmdb"}
	tvdb := &stubTVProvider{
		name: "tvdb",
		searchResults: []MatchResult{
			{Title: "Show", Provider: "tvdb", ExternalID: "series-55"},
		},
		episodeResult: &MatchResult{
			Title:      "Show - S01E03 - Episode",
			Provider:   "tvdb",
			ExternalID: "series-55",
		},
	}
	p := &Pipeline{tvProviders: []TVProvider{tmdb, tvdb}}

	res := p.IdentifyTV(context.Background(), MediaInfo{Title: "Show", TVDBID: "series-55", Season: 1, Episode: 3})
	if res == nil {
		t.Fatal("expected match")
	}
	if len(tmdb.episodeCalls) != 0 {
		t.Fatalf("tmdb should not be used for tvdb match, calls = %#v", tmdb.episodeCalls)
	}
	if len(tvdb.episodeCalls) != 1 || tvdb.episodeCalls[0] != "series-55" {
		t.Fatalf("tvdb episode lookup calls = %#v", tvdb.episodeCalls)
	}
}

func TestIdentifyAnime_DoesNotFallbackToTVDBSearchWhenTMDBDoesNotResolve(t *testing.T) {
	tmdb := &stubTVProvider{
		name: "tmdb",
	}
	tvdb := &stubTVProvider{
		name: "tvdb",
		searchResults: []MatchResult{
			{Title: "Frieren", Provider: "tvdb", ExternalID: "series-55"},
		},
		episodeResult: &MatchResult{
			Title:      "Frieren - S01E12 - Episode",
			Provider:   "tvdb",
			ExternalID: "series-55",
		},
	}
	p := &Pipeline{tvProviders: []TVProvider{tmdb, tvdb}}

	res := p.IdentifyAnime(context.Background(), MediaInfo{Title: "Frieren", Season: 1, Episode: 12})
	if res != nil {
		t.Fatalf("expected no match, got %#v", res)
	}
	if len(tmdb.episodeCalls) != 0 {
		t.Fatalf("tmdb episode lookup calls = %#v", tmdb.episodeCalls)
	}
	if len(tvdb.episodeCalls) != 0 {
		t.Fatalf("tvdb should not be used, calls = %#v", tvdb.episodeCalls)
	}
}

func TestIdentifyAnime_UsesTMDBResultEvenWhenTVDBAlsoMatches(t *testing.T) {
	tmdb := &stubTVProvider{
		name: "tmdb",
		searchResults: []MatchResult{
			{Title: "Frieren", Provider: "tmdb", ExternalID: "10"},
		},
		episodeResult: &MatchResult{
			Title:      "Frieren - S01E12 - Episode",
			Provider:   "tmdb",
			ExternalID: "10",
		},
	}
	tvdb := &stubTVProvider{
		name: "tvdb",
		searchResults: []MatchResult{
			{Title: "Frieren", Provider: "tvdb", ExternalID: "series-55"},
		},
	}
	p := &Pipeline{tvProviders: []TVProvider{tmdb, tvdb}}

	res := p.IdentifyAnime(context.Background(), MediaInfo{Title: "Frieren", Season: 1, Episode: 12})
	if res == nil {
		t.Fatal("expected match")
	}
	if res.Provider != "tmdb" {
		t.Fatalf("provider = %q", res.Provider)
	}
	if len(tmdb.episodeCalls) != 1 || tmdb.episodeCalls[0] != "10" {
		t.Fatalf("tmdb episode lookup calls = %#v", tmdb.episodeCalls)
	}
	if len(tvdb.episodeCalls) != 0 {
		t.Fatalf("tvdb episode lookup calls = %#v", tvdb.episodeCalls)
	}
}

func TestIdentifyAnime_ExplicitTMDBIDUsesEpisodeMetadata(t *testing.T) {
	tmdb := &stubTVProvider{
		name: "tmdb",
		episodeResult: &MatchResult{
			Title:      "Frieren - S01E12 - Episode",
			Provider:   "tmdb",
			ExternalID: "123",
		},
	}
	p := &Pipeline{
		tvProviders:           []TVProvider{tmdb},
		seriesDetailsProvider: &stubSeriesDetailsProvider{details: &SeriesDetails{Name: "Frieren"}},
	}

	res := p.IdentifyAnime(context.Background(), MediaInfo{TMDBID: 123, Season: 1, Episode: 12})
	if res == nil {
		t.Fatal("expected match")
	}
	if res.Provider != "tmdb" {
		t.Fatalf("provider = %q", res.Provider)
	}
	if len(tmdb.episodeCalls) != 1 || tmdb.episodeCalls[0] != "123" {
		t.Fatalf("episode lookup calls = %#v", tmdb.episodeCalls)
	}
}

func TestIdentifyAnime_ExplicitTVDBIDDoesNotBypassTMDBOnlyLookup(t *testing.T) {
	tvdb := &stubTVProvider{
		name: "tvdb",
		episodeResult: &MatchResult{
			Title:      "Frieren - S01E12 - Episode",
			Provider:   "tvdb",
			ExternalID: "series-55",
		},
	}
	p := &Pipeline{tvProviders: []TVProvider{tvdb}}

	res := p.IdentifyAnime(context.Background(), MediaInfo{Title: "Frieren", TVDBID: "series-55", Season: 1, Episode: 12})
	if res != nil {
		t.Fatalf("expected no match, got %#v", res)
	}
	if len(tvdb.episodeCalls) != 0 {
		t.Fatalf("tvdb episode lookup calls = %#v", tvdb.episodeCalls)
	}
}

func TestIdentifyAnime_AbsoluteEpisodeOnlyReturnsNoMatch(t *testing.T) {
	tmdb := &stubTVProvider{
		name: "tmdb",
		searchResults: []MatchResult{
			{Title: "Frieren", Provider: "tmdb", ExternalID: "10"},
		},
	}
	p := &Pipeline{tvProviders: []TVProvider{tmdb}}

	res := p.IdentifyAnime(context.Background(), MediaInfo{Title: "Frieren", AbsoluteEpisode: 12})
	if res != nil {
		t.Fatalf("expected no match, got %#v", res)
	}
}

func TestIdentifyMovie_ExactTitleAndYearAutoMatches(t *testing.T) {
	movie := &stubMovieProvider{
		searchResults: []MatchResult{
			{Title: "Die My Love", ReleaseDate: "2025-01-01", Provider: "tmdb", ExternalID: "444"},
		},
	}
	p := &Pipeline{movieProvider: movie}

	res := p.IdentifyMovie(context.Background(), MediaInfo{Title: "Die My Love", Year: 2025})
	if res == nil {
		t.Fatal("expected match")
	}
	if res.Title != "Die My Love" {
		t.Fatalf("title = %q", res.Title)
	}
}

func TestIdentifyMovie_ConflictingYearStaysUnmatched(t *testing.T) {
	movie := &stubMovieProvider{
		searchResults: []MatchResult{
			{Title: "Die My Love", ReleaseDate: "2024-01-01", Provider: "tmdb", ExternalID: "444"},
		},
	}
	p := &Pipeline{movieProvider: movie}

	res := p.IdentifyMovie(context.Background(), MediaInfo{Title: "Die My Love", Year: 2025})
	if res != nil {
		t.Fatalf("expected no match, got %#v", res)
	}
}

func TestIdentifyMovie_ExplicitTMDBIDUsesLookup(t *testing.T) {
	movie := &stubMovieProvider{
		lookupResult: &MatchResult{Title: "Movie", Provider: "tmdb", ExternalID: "444"},
	}
	p := &Pipeline{movieProvider: movie}

	res := p.IdentifyMovie(context.Background(), MediaInfo{TMDBID: 444})
	if res == nil {
		t.Fatal("expected match")
	}
	if res.Title != "Movie" {
		t.Fatalf("title = %q", res.Title)
	}
	if len(movie.lookupCalls) != 1 || movie.lookupCalls[0] != "444" {
		t.Fatalf("lookup calls = %#v", movie.lookupCalls)
	}
}
