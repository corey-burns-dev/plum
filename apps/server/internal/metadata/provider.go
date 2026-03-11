package metadata

import "context"

// Identifier is implemented by Pipeline and by test mocks. Used by the scanner to resolve metadata.
type Identifier interface {
	IdentifyTV(ctx context.Context, info MediaInfo) *MatchResult
	IdentifyAnime(ctx context.Context, info MediaInfo) *MatchResult
	IdentifyMovie(ctx context.Context, info MediaInfo) *MatchResult
}

// MatchResult is a provider-agnostic metadata result for a movie or TV episode.
// PosterURL and BackdropURL are full URLs so the pipeline owns URL shape.
type MatchResult struct {
	Title       string
	Overview    string
	PosterURL   string
	BackdropURL string
	ReleaseDate string
	VoteAverage float64
	IMDbID      string
	IMDbRating  float64
	Provider    string // e.g. "tmdb", "tvdb"
	ExternalID  string // provider-specific id (string supports both TMDB int and TVDB string)
}

// TVProvider searches for TV series and fetches episode details.
type TVProvider interface {
	ProviderName() string
	SearchTV(ctx context.Context, query string) ([]MatchResult, error)
	GetEpisode(ctx context.Context, seriesID string, season, episode int) (*MatchResult, error)
}

// MovieProvider searches for movies.
type MovieProvider interface {
	SearchMovie(ctx context.Context, query string) ([]MatchResult, error)
}

// MovieLookupProvider can resolve a movie directly by provider ID.
type MovieLookupProvider interface {
	GetMovie(ctx context.Context, movieID string) (*MatchResult, error)
}

// SeriesDetails is minimal TV series info for the show-detail UI.
type SeriesDetails struct {
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`   // full URL or path
	BackdropPath string `json:"backdrop_path"` // full URL or path
	FirstAirDate string `json:"first_air_date"`
}

// SeriesDetailsProvider fetches TV series metadata by TMDB ID.
type SeriesDetailsProvider interface {
	GetSeriesDetails(ctx context.Context, tmdbID int) (*SeriesDetails, error)
}

// IMDbRatingProvider resolves an IMDb rating by IMDb title id.
type IMDbRatingProvider interface {
	GetIMDbRatingByID(ctx context.Context, imdbID string) (float64, error)
}
