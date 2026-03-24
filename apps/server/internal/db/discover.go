package db

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"plum/internal/metadata"
)

func AttachDiscoverLibraryMatches(dbConn *sql.DB, userID int, items []metadata.DiscoverItem) error {
	if len(items) == 0 {
		return nil
	}

	movieIDs := make([]int, 0, len(items))
	tvIDs := make([]int, 0, len(items))
	for _, item := range items {
		if item.TMDBID <= 0 {
			continue
		}
		switch item.MediaType {
		case metadata.DiscoverMediaTypeMovie:
			movieIDs = append(movieIDs, item.TMDBID)
		case metadata.DiscoverMediaTypeTV:
			tvIDs = append(tvIDs, item.TMDBID)
		}
	}

	movieMatches, err := discoverMovieMatches(dbConn, userID, movieIDs)
	if err != nil {
		return err
	}
	tvMatches, err := discoverTVMatches(dbConn, userID, tvIDs)
	if err != nil {
		return err
	}

	for i := range items {
		switch items[i].MediaType {
		case metadata.DiscoverMediaTypeMovie:
			items[i].LibraryMatches = cloneDiscoverLibraryMatches(movieMatches[items[i].TMDBID])
		case metadata.DiscoverMediaTypeTV:
			items[i].LibraryMatches = cloneDiscoverLibraryMatches(tvMatches[items[i].TMDBID])
		}
	}
	return nil
}

func AttachDiscoverTitleLibraryMatches(dbConn *sql.DB, userID int, details *metadata.DiscoverTitleDetails) error {
	if details == nil || details.TMDBID <= 0 {
		return nil
	}
	switch details.MediaType {
	case metadata.DiscoverMediaTypeMovie:
		matches, err := discoverMovieMatches(dbConn, userID, []int{details.TMDBID})
		if err != nil {
			return err
		}
		details.LibraryMatches = cloneDiscoverLibraryMatches(matches[details.TMDBID])
	case metadata.DiscoverMediaTypeTV:
		matches, err := discoverTVMatches(dbConn, userID, []int{details.TMDBID})
		if err != nil {
			return err
		}
		details.LibraryMatches = cloneDiscoverLibraryMatches(matches[details.TMDBID])
	}
	return nil
}

func discoverMovieMatches(dbConn *sql.DB, userID int, tmdbIDs []int) (map[int][]metadata.DiscoverLibraryMatch, error) {
	result := make(map[int][]metadata.DiscoverLibraryMatch)
	ids := uniquePositiveInts(tmdbIDs)
	if len(ids) == 0 {
		return result, nil
	}

	query := fmt.Sprintf(
		`SELECT COALESCE(m.tmdb_id, 0), l.id, l.name, l.type
FROM movies m
JOIN libraries l ON l.id = m.library_id
WHERE l.user_id = ? AND COALESCE(m.missing_since, '') = '' AND COALESCE(m.tmdb_id, 0) IN (%s)
GROUP BY COALESCE(m.tmdb_id, 0), l.id, l.name, l.type
ORDER BY l.name, l.id`,
		intPlaceholders(len(ids)),
	)
	args := make([]any, 0, len(ids)+1)
	args = append(args, userID)
	for _, id := range ids {
		args = append(args, id)
	}

	rows, err := dbConn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var tmdbID int
		var match metadata.DiscoverLibraryMatch
		if err := rows.Scan(&tmdbID, &match.LibraryID, &match.LibraryName, &match.LibraryType); err != nil {
			return nil, err
		}
		match.Kind = "movie"
		result[tmdbID] = append(result[tmdbID], match)
	}
	return result, rows.Err()
}

func discoverTVMatches(dbConn *sql.DB, userID int, tmdbIDs []int) (map[int][]metadata.DiscoverLibraryMatch, error) {
	result := make(map[int][]metadata.DiscoverLibraryMatch)
	ids := uniquePositiveInts(tmdbIDs)
	if len(ids) == 0 {
		return result, nil
	}

	appendMatches := func(query string, args []any) error {
		rows, err := dbConn.Query(query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var tmdbID int
			var match metadata.DiscoverLibraryMatch
			if err := rows.Scan(&tmdbID, &match.LibraryID, &match.LibraryName, &match.LibraryType); err != nil {
				return err
			}
			match.Kind = "show"
			match.ShowKey = fmt.Sprintf("tmdb-%d", tmdbID)
			result[tmdbID] = append(result[tmdbID], match)
		}
		return rows.Err()
	}

	showArgs := make([]any, 0, len(ids)+3)
	showArgs = append(showArgs, userID, LibraryTypeTV, LibraryTypeAnime)
	for _, id := range ids {
		showArgs = append(showArgs, id)
	}
	showQuery := fmt.Sprintf(
		`SELECT COALESCE(s.tmdb_id, 0), l.id, l.name, l.type
FROM shows s
JOIN libraries l ON l.id = s.library_id
WHERE l.user_id = ? AND s.kind IN (?, ?) AND COALESCE(s.tmdb_id, 0) IN (%s)
GROUP BY COALESCE(s.tmdb_id, 0), l.id, l.name, l.type
ORDER BY l.name, l.id`,
		intPlaceholders(len(ids)),
	)
	if err := appendMatches(showQuery, showArgs); err != nil {
		return nil, err
	}

	tvArgs := make([]any, 0, len(ids)+1)
	tvArgs = append(tvArgs, userID)
	for _, id := range ids {
		tvArgs = append(tvArgs, id)
	}
	tvQuery := fmt.Sprintf(
		`SELECT COALESCE(m.tmdb_id, 0), l.id, l.name, l.type
FROM tv_episodes m
JOIN libraries l ON l.id = m.library_id
WHERE l.user_id = ? AND COALESCE(m.missing_since, '') = '' AND COALESCE(m.tmdb_id, 0) IN (%s)
GROUP BY COALESCE(m.tmdb_id, 0), l.id, l.name, l.type
ORDER BY l.name, l.id`,
		intPlaceholders(len(ids)),
	)
	if err := appendMatches(tvQuery, tvArgs); err != nil {
		return nil, err
	}

	animeQuery := fmt.Sprintf(
		`SELECT COALESCE(m.tmdb_id, 0), l.id, l.name, l.type
FROM anime_episodes m
JOIN libraries l ON l.id = m.library_id
WHERE l.user_id = ? AND COALESCE(m.missing_since, '') = '' AND COALESCE(m.tmdb_id, 0) IN (%s)
GROUP BY COALESCE(m.tmdb_id, 0), l.id, l.name, l.type
ORDER BY l.name, l.id`,
		intPlaceholders(len(ids)),
	)
	if err := appendMatches(animeQuery, tvArgs); err != nil {
		return nil, err
	}

	for tmdbID, matches := range result {
		result[tmdbID] = dedupeDiscoverMatches(matches)
	}
	return result, nil
}

func dedupeDiscoverMatches(matches []metadata.DiscoverLibraryMatch) []metadata.DiscoverLibraryMatch {
	if len(matches) <= 1 {
		return matches
	}
	type key struct {
		libraryID int
		kind      string
		showKey   string
	}
	seen := make(map[key]bool, len(matches))
	out := make([]metadata.DiscoverLibraryMatch, 0, len(matches))
	for _, match := range matches {
		matchKey := key{
			libraryID: match.LibraryID,
			kind:      match.Kind,
			showKey:   match.ShowKey,
		}
		if seen[matchKey] {
			continue
		}
		seen[matchKey] = true
		out = append(out, match)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LibraryName == out[j].LibraryName {
			return out[i].LibraryID < out[j].LibraryID
		}
		return out[i].LibraryName < out[j].LibraryName
	})
	return out
}

func cloneDiscoverLibraryMatches(matches []metadata.DiscoverLibraryMatch) []metadata.DiscoverLibraryMatch {
	if len(matches) == 0 {
		return nil
	}
	out := make([]metadata.DiscoverLibraryMatch, len(matches))
	copy(out, matches)
	return out
}

func uniquePositiveInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int]bool, len(values))
	out := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Ints(out)
	return out
}

func intPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	parts := make([]string, count)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}
