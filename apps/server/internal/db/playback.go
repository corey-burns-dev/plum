package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	completedProgressPercent = 95.0
	completedRemainingSecs   = 120.0
	recentlyAddedLimit       = 24
)

type ContinueWatchingEntry struct {
	Kind             string    `json:"kind"`
	Media            MediaItem `json:"media"`
	ShowKey          string    `json:"show_key,omitempty"`
	ShowTitle        string    `json:"show_title,omitempty"`
	EpisodeLabel     string    `json:"episode_label,omitempty"`
	RemainingSeconds float64   `json:"remaining_seconds"`
	activityAt       string
}

type RecentlyAddedEntry struct {
	Kind         string    `json:"kind"`
	Media        MediaItem `json:"media"`
	ShowKey      string    `json:"show_key,omitempty"`
	ShowTitle    string    `json:"show_title,omitempty"`
	EpisodeLabel string    `json:"episode_label,omitempty"`
}

type HomeDashboard struct {
	ContinueWatching []ContinueWatchingEntry `json:"continueWatching"`
	RecentlyAdded    []RecentlyAddedEntry    `json:"recentlyAdded"`
}

type playbackProgressRow struct {
	PositionSeconds float64
	DurationSeconds float64
	ProgressPercent float64
	Completed       bool
	LastWatchedAt   string
}

func GetMediaByLibraryIDForUser(db *sql.DB, libraryID int, userID int) ([]MediaItem, error) {
	items, err := GetMediaByLibraryID(db, libraryID)
	if err != nil {
		return nil, err
	}
	return attachPlaybackProgressBatch(db, userID, items)
}

func GetHomeDashboardForUser(db *sql.DB, userID int) (HomeDashboard, error) {
	items, err := queryAllMediaByKind(db, userID, "")
	if err != nil {
		return HomeDashboard{}, err
	}
	items, err = attachPlaybackProgressBatch(db, userID, items)
	if err != nil {
		return HomeDashboard{}, err
	}
	continueWatching := buildContinueWatching(items)
	recentlyAdded := buildRecentlyAdded(items)
	continueWatching, recentlyAdded, err = attachDashboardEntrySubtitles(db, continueWatching, recentlyAdded)
	if err != nil {
		return HomeDashboard{}, err
	}

	return HomeDashboard{
		ContinueWatching: continueWatching,
		RecentlyAdded:    recentlyAdded,
	}, nil
}

func attachDashboardEntrySubtitles(db *sql.DB, continueWatching []ContinueWatchingEntry, recentlyAdded []RecentlyAddedEntry) ([]ContinueWatchingEntry, []RecentlyAddedEntry, error) {
	uniqueMedia := make(map[int]MediaItem, len(continueWatching)+len(recentlyAdded))
	for i := range continueWatching {
		uniqueMedia[continueWatching[i].Media.ID] = continueWatching[i].Media
	}
	for i := range recentlyAdded {
		uniqueMedia[recentlyAdded[i].Media.ID] = recentlyAdded[i].Media
	}

	items := make([]MediaItem, 0, len(uniqueMedia))
	for _, item := range uniqueMedia {
		items = append(items, item)
	}
	items, err := attachSubtitlesBatch(db, items)
	if err != nil {
		return nil, nil, err
	}

	mediaByID := make(map[int]MediaItem, len(items))
	for _, item := range items {
		mediaByID[item.ID] = item
	}
	for i := range continueWatching {
		if item, ok := mediaByID[continueWatching[i].Media.ID]; ok {
			continueWatching[i].Media = item
		}
	}
	for i := range recentlyAdded {
		if item, ok := mediaByID[recentlyAdded[i].Media.ID]; ok {
			recentlyAdded[i].Media = item
		}
	}
	return continueWatching, recentlyAdded, nil
}

func buildContinueWatching(items []MediaItem) []ContinueWatchingEntry {
	movies := make([]ContinueWatchingEntry, 0)
	showItems := make(map[string][]MediaItem)
	for _, item := range items {
		if item.Type == LibraryTypeMusic {
			continue
		}
		if item.Type == LibraryTypeMovie {
			if item.Completed || item.ProgressPercent <= 0 {
				continue
			}
			entry := ContinueWatchingEntry{
				Kind:             "movie",
				Media:            item,
				RemainingSeconds: item.RemainingSeconds,
				activityAt:       item.LastWatchedAt,
			}
			movies = append(movies, entry)
			continue
		}
		if item.Type != LibraryTypeTV && item.Type != LibraryTypeAnime {
			continue
		}
		key := showKeyFromItem(item.TMDBID, item.Title)
		showItems[key] = append(showItems[key], item)
	}

	entries := make([]ContinueWatchingEntry, 0, len(movies)+len(showItems))
	entries = append(entries, movies...)
	for showKey, episodes := range showItems {
		if entry, ok := continueWatchingEntryForShow(showKey, episodes); ok {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].activityAt > entries[j].activityAt
	})

	return entries
}

func buildRecentlyAdded(items []MediaItem) []RecentlyAddedEntry {
	entries := make([]RecentlyAddedEntry, 0)
	showItems := make(map[string][]MediaItem)
	for _, item := range items {
		if item.Type == LibraryTypeMusic {
			continue
		}
		if item.Type == LibraryTypeMovie {
			entries = append(entries, RecentlyAddedEntry{
				Kind:  "movie",
				Media: item,
			})
			continue
		}
		if item.Type != LibraryTypeTV && item.Type != LibraryTypeAnime {
			continue
		}
		key := showKeyFromItem(item.TMDBID, item.Title)
		showItems[key] = append(showItems[key], item)
	}
	for showKey, episodes := range showItems {
		entry, ok := recentlyAddedEntryForShow(showKey, episodes)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Media.ID != entries[j].Media.ID {
			return entries[i].Media.ID > entries[j].Media.ID
		}
		return entries[i].Media.Title < entries[j].Media.Title
	})
	if len(entries) > recentlyAddedLimit {
		return entries[:recentlyAddedLimit]
	}
	return entries
}

func continueWatchingEntryForShow(showKey string, episodes []MediaItem) (ContinueWatchingEntry, bool) {
	if len(episodes) == 0 {
		return ContinueWatchingEntry{}, false
	}
	sort.Slice(episodes, func(i, j int) bool {
		if episodes[i].Season != episodes[j].Season {
			return episodes[i].Season < episodes[j].Season
		}
		if episodes[i].Episode != episodes[j].Episode {
			return episodes[i].Episode < episodes[j].Episode
		}
		return episodes[i].Title < episodes[j].Title
	})

	var partial *MediaItem
	for i := range episodes {
		if episodes[i].Completed || episodes[i].ProgressPercent <= 0 {
			continue
		}
		if partial == nil || partial.LastWatchedAt < episodes[i].LastWatchedAt {
			partial = &episodes[i]
		}
	}
	if partial != nil {
		return buildShowContinueWatchingEntry(showKey, *partial, partial.LastWatchedAt), true
	}

	var latestCompletedIndex = -1
	var latestCompletedAt string
	for i := range episodes {
		if !episodes[i].Completed || episodes[i].LastWatchedAt == "" {
			continue
		}
		if latestCompletedIndex < 0 || latestCompletedAt < episodes[i].LastWatchedAt {
			latestCompletedIndex = i
			latestCompletedAt = episodes[i].LastWatchedAt
		}
	}
	if latestCompletedIndex < 0 {
		return ContinueWatchingEntry{}, false
	}
	for i := latestCompletedIndex + 1; i < len(episodes); i++ {
		if episodes[i].Completed {
			continue
		}
		return buildShowContinueWatchingEntry(showKey, episodes[i], latestCompletedAt), true
	}
	return ContinueWatchingEntry{}, false
}

func buildShowContinueWatchingEntry(showKey string, item MediaItem, activityAt string) ContinueWatchingEntry {
	return ContinueWatchingEntry{
		Kind:             "show",
		Media:            item,
		ShowKey:          showKey,
		ShowTitle:        showTitleFromEpisodeTitle(item.Title),
		EpisodeLabel:     episodeLabel(item),
		RemainingSeconds: item.RemainingSeconds,
		activityAt:       activityAt,
	}
}

func recentlyAddedEntryForShow(showKey string, episodes []MediaItem) (RecentlyAddedEntry, bool) {
	if len(episodes) == 0 {
		return RecentlyAddedEntry{}, false
	}
	newest := episodes[0]
	for i := 1; i < len(episodes); i++ {
		if episodes[i].ID > newest.ID {
			newest = episodes[i]
		}
	}
	return RecentlyAddedEntry{
		Kind:         "show",
		Media:        newest,
		ShowKey:      showKey,
		ShowTitle:    showTitleFromEpisodeTitle(newest.Title),
		EpisodeLabel: episodeLabel(newest),
	}, true
}

func UpsertPlaybackProgress(db *sql.DB, userID, mediaID int, positionSeconds, durationSeconds float64, completed bool) error {
	if userID <= 0 || mediaID <= 0 {
		return fmt.Errorf("user and media ids are required")
	}
	if positionSeconds < 0 {
		positionSeconds = 0
	}
	if durationSeconds < 0 {
		durationSeconds = 0
	}
	progressPercent := 0.0
	if durationSeconds > 0 {
		progressPercent = (positionSeconds / durationSeconds) * 100
		if progressPercent < 0 {
			progressPercent = 0
		}
		if progressPercent > 100 {
			progressPercent = 100
		}
	}
	remainingSeconds := durationSeconds - positionSeconds
	if remainingSeconds < 0 {
		remainingSeconds = 0
	}
	if !completed && (progressPercent >= completedProgressPercent || (durationSeconds > 0 && remainingSeconds <= completedRemainingSecs)) {
		completed = true
	}
	if completed {
		positionSeconds = 0
		progressPercent = 100
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
INSERT INTO playback_progress (
  user_id, media_id, position_seconds, duration_seconds, progress_percent, completed, last_watched_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id, media_id) DO UPDATE SET
  position_seconds = excluded.position_seconds,
  duration_seconds = excluded.duration_seconds,
  progress_percent = excluded.progress_percent,
  completed = excluded.completed,
  last_watched_at = excluded.last_watched_at,
  updated_at = excluded.updated_at
`,
		userID,
		mediaID,
		positionSeconds,
		durationSeconds,
		progressPercent,
		completed,
		now,
		now,
		now,
	)
	return err
}

func attachPlaybackProgressBatch(db *sql.DB, userID int, items []MediaItem) ([]MediaItem, error) {
	if userID <= 0 || len(items) == 0 {
		return items, nil
	}
	ids := make([]int, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	progressByID, err := getPlaybackProgressByMediaIDs(db, userID, ids)
	if err != nil {
		return nil, err
	}
	for i := range items {
		progress, ok := progressByID[items[i].ID]
		if !ok {
			continue
		}
		items[i].ProgressSeconds = progress.PositionSeconds
		items[i].ProgressPercent = progress.ProgressPercent
		items[i].Completed = progress.Completed
		items[i].LastWatchedAt = progress.LastWatchedAt
		if duration := progress.DurationSeconds; duration > 0 {
			remaining := duration - progress.PositionSeconds
			if remaining < 0 {
				remaining = 0
			}
			items[i].RemainingSeconds = remaining
		}
	}
	return items, nil
}

func getPlaybackProgressByMediaIDs(db *sql.DB, userID int, mediaIDs []int) (map[int]playbackProgressRow, error) {
	if len(mediaIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(mediaIDs))
	args := make([]any, 0, len(mediaIDs)+1)
	args = append(args, userID)
	for i, mediaID := range mediaIDs {
		placeholders[i] = "?"
		args = append(args, mediaID)
	}
	rows, err := db.Query(
		`SELECT media_id, position_seconds, duration_seconds, progress_percent, completed, COALESCE(last_watched_at, '') FROM playback_progress WHERE user_id = ? AND media_id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int]playbackProgressRow, len(mediaIDs))
	for rows.Next() {
		var mediaID int
		var progress playbackProgressRow
		if err := rows.Scan(&mediaID, &progress.PositionSeconds, &progress.DurationSeconds, &progress.ProgressPercent, &progress.Completed, &progress.LastWatchedAt); err != nil {
			return nil, err
		}
		out[mediaID] = progress
	}
	return out, rows.Err()
}

func showTitleFromEpisodeTitle(title string) string {
	if i := strings.Index(strings.ToLower(title), " - s"); i > 0 {
		return strings.TrimSpace(title[:i])
	}
	if i := strings.Index(title, " - "); i > 0 {
		return strings.TrimSpace(title[:i])
	}
	return strings.TrimSpace(title)
}

func episodeLabel(item MediaItem) string {
	season := item.Season
	episode := item.Episode
	if season <= 0 && episode <= 0 {
		return ""
	}
	return fmt.Sprintf("S%02dE%02d", season, episode)
}
