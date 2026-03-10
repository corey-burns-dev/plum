Your plan is already strong. The main thing missing is that **TVDB should not just appear as a fallback** — in real media servers it is often **a primary provider for TV content** alongside TMDB.

Below is a **clean revised version of your plan** with **TVDB properly integrated into the architecture**, not just mentioned.

---

# Metadata Strategy for Plum

If you want **Plex/Emby-level media identification**, you should **never rely on only one metadata source**. Even Plex and Emby combine multiple providers and fallback systems.

The correct approach is to build a **metadata provider pipeline**, not a single API integration.

Plum will integrate multiple metadata providers, including **The Movie Database and TheTVDB**, to maximize match accuracy and coverage.

---

# Major Metadata Sources

## 1. TMDB (Primary Movies + TV)

The Movie Database

One of the largest open metadata sources for movies and television.

**Provides**

* movies
* TV shows
* episodes
* posters / backdrops
* cast lists
* release dates
* genres

**Pros**

* free API
* excellent artwork
* strong movie coverage
* good global community support

**Cons**

* TV episode numbering sometimes inconsistent
* anime coverage can vary

**Typical use**

* primary provider for **movies**
* primary search provider for **TV shows**

---

## 2. TVDB (Primary TV Metadata)

TheTVDB

One of the most authoritative datasets for **television series metadata**.

**Provides**

* show information
* season and episode data
* alternate episode orderings
* artwork
* actors
* air dates
* multiple numbering schemes

**Pros**

* extremely strong TV coverage
* excellent episode numbering consistency
* widely used in media servers
* strong anime / niche TV coverage

**Cons**

* API requires a **paid license**
* artwork library smaller than TMDB

**Typical use**

* primary or secondary **TV metadata provider**
* episode numbering verification
* fallback matching when TMDB fails

---

## 3. OMDb

OMDb API

OMDb provides metadata derived from **IMDb**.

**Provides**

* movie metadata
* ratings
* posters
* actors
* release dates

**Pros**

* simple API
* good movie identification
* provides IMDb IDs

**Cons**

* smaller dataset than TMDB
* limited artwork

---

## 4. IMDb Datasets

IMDb

IMDb offers downloadable datasets for large-scale metadata usage.

**Datasets include**

* titles
* ratings
* cast
* release years

**Pros**

* largest movie/TV dataset available
* authoritative identifiers

**Cons**

* integration more complex
* not designed as a simple API

---

## 5. OpenSubtitles (Identification via Hash)

OpenSubtitles

Subtitle databases can be used to **identify media through file hashing**.

**Technique used by media servers**

1. compute video file hash
2. query subtitle database
3. infer movie/episode identity

**Pros**

* works when filenames are messy
* useful fallback

---

## 6. Anime Metadata Providers

For anime libraries Plum may integrate:

* AniDB
* MyAnimeList

**Provides**

* anime-specific episode ordering
* alternate titles
* release groups
* specialized metadata

---

## 7. Music Metadata

For music libraries Plum may integrate:

MusicBrainz

**Provides**

* artist data
* album metadata
* track identification
* acoustic fingerprinting

---

# Identification Techniques

Metadata APIs alone are not enough.

Plum will combine **metadata providers with media analysis techniques**.

---

## Filename Parsing

Media filenames often contain structured information.

Example:

```
Show.Name.S02E05.1080p.WEB-DL.mkv
```

Extract:

```
title
season
episode
year
resolution
source
```

These values guide metadata queries.

---

## Video Fingerprinting

Using **FFmpeg / ffprobe** Plum can extract:

* duration
* codec
* resolution
* audio tracks
* container metadata

This data helps validate metadata matches.

---

## File Hash Identification

Used for subtitle databases and secondary matching.

Common hashes:

* OpenSubtitles hash
* MD5
* SHA1

Typical strategy:

```
hash first 64KB
hash last 64KB
combine values
```

---

# Artwork Sources

Artwork will be fetched from multiple providers.

Primary sources:

* The Movie Database
* TheTVDB
* Fanart.tv

Artwork types:

* posters
* backdrops
* clear logos
* disc art

---

# A Plex-Style Metadata Pipeline

Plum’s identification pipeline will work roughly like this.

### Step 1 — Parse filename

```
Show.Name.S02E05.1080p.WEB-DL.mkv
```

Extract metadata hints.

---

### Step 2 — Query primary metadata provider

```
TMDB
```

Match movie or series.

---

### Step 3 — Cross-check with TVDB (for television)

```
TVDB lookup
```

Validate:

* season numbering
* episode numbers
* episode titles

---

### Step 4 — Use fallback providers

```
OMDb
IMDb dataset
OpenSubtitles
```

---

### Step 5 — Validate using media probe

Using ffprobe:

```
duration
resolution
audio tracks
```

Ensure metadata consistency.

---

### Step 6 — Fetch artwork

```
TMDB
TVDB
fanart.tv
```

---

### Step 7 — Cache metadata locally

Database:

```
SQLite
Postgres
```

Stored data:

```
titles
provider IDs
poster URLs
season/episode data
cast
genres
runtime
```

---

# Key Architecture Idea

The system should not think:

```
metadata = TMDB
```

Instead:

```
metadata system =
    filename parser
    metadata providers
    artwork providers
    hashing system
    caching layer
    verification logic
```

---

# Minimum Provider Stack (Plex-Level)

### Movies

```
TMDB
OMDb
IMDb dataset
```

### Television

```
TMDB
TVDB
```

### Artwork

```
TMDB
fanart.tv
TVDB
```

### Subtitles

```
OpenSubtitles
```

---

# Recommended Provider Stack for Plum

```
Metadata:
  TMDB
  TVDB
  OMDb

Artwork:
  TMDB
  fanart.tv
  TVDB

Subtitles:
  OpenSubtitles

Music:
  MusicBrainz
```

---

# Important Engineering Recommendation

Metadata providers should be implemented as **plugins**.

Example structure:

```
providers/
  tmdb/
  tvdb/
  omdb/
  imdb/
  fanart/
  opensubtitles/
```

This allows Plum to:

* enable/disable providers
* add new providers later
* support regional metadata services
* avoid tight coupling to a single API

This plugin architecture is similar to how **Plex, Emby, and Jellyfin scale their metadata systems**.

---

If you want, I can also show you something **very useful for Plum specifically**:

**How Plex/Jellyfin actually score matches internally** (their heuristic matching algorithm). It's one of the most important parts of making a media server feel “smart.”
