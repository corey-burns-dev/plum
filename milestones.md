# Plum Development Roadmap

## Phase 0 — Project Foundations

Goal: create the core skeleton so everything later plugs in cleanly.

### Project setup

- [x] Initialize repo structure
- [x] Backend service skeleton (Go)
- [x] Frontend app scaffold (React/Vite)
- [x] Docker dev environment
- [x] Config system
- [x] Logging system
- [x] Error handling framework
- [x] Environment configuration
- [x] API client SDK (TS)

### Database

- [x] DB schema design
- [x] Migration system
- [x] ORM or query layer
- [x] Database indexing strategy

### Basic API framework

- [x] API routing
- [x] request validation
- [x] response formatting
- [x] auth middleware

---

## Phase 1 — Infrastructure & Library System

Goal: handle background work and detect media on disk.

### Job system

- [x] job queue
- [x] worker pool
- [ ] job retry system
- [x] job priority
- [x] job monitoring

### Library management

- [x] create library
- [x] library types (movies / shows)
- [x] add library folders
- [x] library settings

### Filesystem scanning

- [x] directory scanner
- [x] recursive scanning
- [x] file change detection
- [x] background scan jobs
- [x] manual scan trigger
- [ ] filesystem watcher
- [x] debounce scan triggers
- [x] partial folder scans
- [ ] scan scheduling

### Media file model

- [x] media files table
- [x] file metadata storage
- [x] file hash calculation
- [x] ffprobe integration
- [x] stream detection (video/audio/subtitle)
- [x] missing file detection
- [x] duplicate detection
- [ ] media identity model (items vs files)

---

## Phase 2 — Metadata & Image Pipeline

Goal: turn raw files into identifiable movies/shows and handle assets.

### Filename parsing

- [x] movie parser
- [x] TV episode parser
- [x] season detection
- [x] episode range detection
- [x] anime absolute episode support

### Metadata providers

- [x] TMDB integration
- [x] TVDB integration
- [x] OMDb integration
- [x] provider ID storage
- [x] provider response cache
- [x] metadata refresh policy
- [x] metadata versioning

### Matching engine

- [x] candidate search
- [x] scoring algorithm
- [x] confidence thresholds
- [x] auto match
- [x] unmatched queue

### Metadata storage

- [x] movies table
- [x] shows table
- [x] seasons table
- [x] episodes table
- [x] provider ID mappings

### Image Pipeline

- [x] poster fetching
- [x] backdrop fetching
- [ ] image caching
- [ ] image resizing
- [x] thumbnail generation
- [ ] artwork deduplication
- [ ] CDN-style serving
- [x] artwork prioritization

---

## Phase 3 — Web App Media Browser

Goal: basic UI for browsing media.

### Navigation

- [x] libraries page
- [x] movie grid
- [x] show grid
- [x] show detail page
- [x] season page
- [x] episode page

### Metadata display

- [x] posters
- [x] descriptions
- [ ] cast lists
- [ ] genres
- [x] runtime

### Search System

- [ ] search index
- [ ] title search
- [ ] actor search
- [ ] genre filtering
- [ ] fuzzy search
- [ ] index refresh jobs

---

## Phase 4 — Playback System

Goal: watch media.

### Playback session API

- [x] playback session creation
- [x] playback permissions
- [x] session tracking

### Transcode Decision Engine

- [ ] client capability detection
- [ ] transcode decision engine
- [ ] bitrate adaptation
- [ ] container compatibility

### Video streaming

- [x] direct play
- [x] HLS streaming
- [x] transcoding pipeline
- [x] bitrate profiles

### Custom video player

- [x] custom controls
- [x] subtitle selection
- [x] audio track selection
- [x] fullscreen
- [x] keyboard shortcuts
- [ ] timeline preview thumbnails (scrubbing)

---

## Phase 5 — User System

Goal: multi-user support.

### Accounts

- [x] user accounts
- [x] authentication
- [x] session tokens

### Profiles

- [ ] user profiles
- [ ] profile switching
- [ ] avatar system

### Permissions

- [x] admin vs user roles
- [ ] library restrictions
- [ ] parental controls

---

## Phase 6 — Watch State & Sync

Goal: track viewing behavior across devices.

### Playback State Model

- [x] playback heartbeat
- [x] session recovery
- [ ] multi-device sync

### Progress tracking

- [x] resume position
- [x] watched status
- [ ] watch history

### Discovery features

- [x] continue watching
- [ ] next up episodes
- [x] recently added

---

## Phase 7 — Media Server Features

Goal: parity with typical media servers.

### Playback features

- [ ] intro detection
- [ ] skip intro
- [ ] skip credits
- [ ] next episode autoplay

### Subtitles

- [ ] subtitle downloading
- [ ] subtitle sync adjustment
- [ ] subtitle burn-in

### Transcoding

- [ ] GPU acceleration
- [ ] codec compatibility
- [ ] subtitle burn-in support

---

## Phase 8 — Devices & Remote Streaming

Goal: use Plum outside the web browser.

### Device Management

- [ ] device registration
- [ ] device auth tokens
- [ ] device profile system
- [ ] codec capability mapping
- [ ] resolution limits

### Remote streaming

- [ ] secure remote access
- [ ] signed stream URLs
- [ ] expiring playback tokens
- [ ] bitrate limits
- [ ] adaptive streaming

### Casting & Mobile

- [ ] responsive playback UI
- [ ] touch player controls
- [ ] Chromecast
- [ ] AirPlay

---

## Phase 9 — Admin System

Goal: make the server manageable.

### Dashboard

- [ ] active sessions
- [ ] server stats
- [ ] scan progress

### Media management

- [x] fix match
- [ ] manual metadata editing
- [ ] lock metadata fields
- [ ] refresh metadata
- [x] manual identify
- [ ] orphaned metadata cleanup

### System tools

- [ ] log viewer
- [ ] job queue viewer
- [ ] database health tools
- [ ] configuration export
- [ ] DB backup & restore tools

---

## Phase 10 — Advanced Media Features

Goal: reach Plex/Emby territory.

### Collections & Curation

- [ ] collections
- [ ] watchlists
- [ ] trailer playback
- [ ] theme songs (tv shows)

### Live TV & DVR

- [ ] tuner support
- [ ] channel scanning
- [ ] program guide
- [ ] recording schedules
- [ ] series recording
- [ ] recording management

### Downloads

- [ ] mobile downloads
- [ ] offline sync

---

## Phase 11 — Ecosystem

Goal: expand beyond core media.

### Plugin system

- [ ] plugin API
- [ ] plugin lifecycle
- [ ] metadata provider plugins

### Additional libraries

- [x] music support
- [ ] photo libraries

### Social features

- [ ] watch together
- [ ] activity feed

---

## Phase 12 — Polishing & Reliability

Goal: make the system feel professional.

### Performance

- [ ] query optimization
- [ ] thumbnail pre-generation

### Reliability

- [ ] crash recovery
- [ ] tracing

### UX improvements

- [ ] keyboard navigation
- [ ] TV remote navigation
- [ ] loading states
- [ ] error states
