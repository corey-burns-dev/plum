package metadata

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const tvshowNFO = "tvshow.nfo"

// Kodi-style tvshow.nfo: uniqueid and optional episodeguide JSON.
type nfoRoot struct {
	XMLName      xml.Name      `xml:"tvshow"`
	UniqueIDs    []nfoUniqueID `xml:"uniqueid"`
	EpisodeGuide string        `xml:"episodeguide"`
}

type nfoUniqueID struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

var episodeguideTmdbRegex = regexp.MustCompile(`"tmdb"\s*:\s*"(\d+)"`)
var episodeguideTvdbRegex = regexp.MustCompile(`"tvdb"\s*:\s*"([^"]+)"`)

// ReadShowNFO reads tvshow.nfo from showRootPath (the show folder containing tvshow.nfo).
// Returns tmdbID, tvdbID, and true if at least one ID was found; otherwise zero values and false.
func ReadShowNFO(showRootPath string) (tmdbID int, tvdbID string, ok bool) {
	path := filepath.Join(showRootPath, tvshowNFO)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "", false
	}

	// Try XML uniqueid first
	var root nfoRoot
	if err := xml.Unmarshal(data, &root); err == nil {
		for _, u := range root.UniqueIDs {
			u.Type = strings.ToLower(strings.TrimSpace(u.Type))
			u.Value = strings.TrimSpace(u.Value)
			if u.Type == "tmdb" && u.Value != "" {
				if id, err := strconv.Atoi(u.Value); err == nil {
					tmdbID = id
					ok = true
				}
			}
			if u.Type == "tvdb" && u.Value != "" {
				tvdbID = u.Value
				ok = true
			}
		}
	}

	// Episodeguide JSON (e.g. {"tmdb": "76479", "tvdb": "355567"})
	if root.EpisodeGuide != "" {
		s := strings.TrimSpace(root.EpisodeGuide)
		if idStr := episodeguideTmdbRegex.FindStringSubmatch(s); len(idStr) == 2 {
			if id, err := strconv.Atoi(idStr[1]); err == nil {
				tmdbID = id
				ok = true
			}
		}
		if m := episodeguideTvdbRegex.FindStringSubmatch(s); len(m) == 2 {
			tvdbID = m[1]
			ok = true
		}
	}

	// If XML didn't parse, try raw regex on file (for malformed or partial NFO)
	if !ok {
		content := string(data)
		if m := regexp.MustCompile(`<uniqueid\s+type="tmdb"[^>]*>(\d+)</uniqueid>`).FindStringSubmatch(content); len(m) == 2 {
			if id, err := strconv.Atoi(m[1]); err == nil {
				tmdbID = id
				ok = true
			}
		}
		if m := regexp.MustCompile(`<uniqueid\s+type="tvdb"[^>]*>([^<]+)</uniqueid>`).FindStringSubmatch(content); len(m) == 2 {
			tvdbID = strings.TrimSpace(m[1])
			ok = true
		}
		if idStr := episodeguideTmdbRegex.FindStringSubmatch(content); len(idStr) == 2 {
			if id, err := strconv.Atoi(idStr[1]); err == nil {
				tmdbID = id
				ok = true
			}
		}
	}

	return tmdbID, tvdbID, ok
}

// ShowRootPath returns the absolute path of the show root folder (first segment under library root)
// given the library root and the absolute path of an episode file.
// Example: root=/lib, path=/lib/Unfamiliar (2026)/Season 1/S01E01.mkv => /lib/Unfamiliar (2026)
func ShowRootPath(libraryRoot, episodePath string) string {
	rel, err := filepath.Rel(libraryRoot, episodePath)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(strings.Trim(filepath.Clean(rel), "/\\"))
	if rel == "" || rel == ".." || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(rel, "/")
	// Drop filename if last segment has a dot
	if len(parts) > 0 && strings.Contains(parts[len(parts)-1], ".") {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return ""
	}
	firstSegment := parts[0]
	return filepath.Join(libraryRoot, firstSegment)
}

// ApplyShowNFO sets info.TMDBID and info.TVDBID from tvshow.nfo when present.
// showRootPath is the show folder path (e.g. from ShowRootPath).
func ApplyShowNFO(info *MediaInfo, showRootPath string) {
	if info == nil || showRootPath == "" {
		return
	}
	tmdb, tvdb, ok := ReadShowNFO(showRootPath)
	if !ok {
		return
	}
	if tmdb > 0 {
		info.TMDBID = tmdb
	}
	if tvdb != "" {
		info.TVDBID = tvdb
	}
}
