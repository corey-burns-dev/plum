# Plum Development Roadmap

## Phase 0 — Project Foundations

Goal: create the core skeleton so everything later plugs in cleanly.

**Project setup**

* [x] Initialize repo structure
* [x] Backend service skeleton (Go)
* [x] Frontend app scaffold (React/Vite)
* [x] Docker dev environment
* [x] Config system
* [x] Logging system
* [x] Error handling framework
* [x] Environment configuration

**Database**

* [x] DB schema design
* [ ] Migration system
* [x] ORM or query layer
* [x] seed/test data support

**Basic API framework**

* [x] API routing
* [x] request validation
* [x] response formatting
* [x] auth middleware
* [ ] API versioning strategy

---

# Phase 1 — File & Library System

Goal: detect media on disk and represent it in the system.

**Library management**

* [x] create library
* [x] library types (movies / shows)
* [x] add library folders
* [x] library settings

**Filesystem scanning**

* [x] directory scanner
* [x] recursive scanning
* [ ] file change detection
* [ ] background scan jobs
* [x] manual scan trigger

**Media file model**

* [x] media files table
* [x] file metadata storage
* [ ] file hash calculation
* [x] ffprobe integration
* [x] stream detection (video/audio/subtitle)

---

# Phase 2 — Metadata Matching System

Goal: turn raw files into identifiable movies/shows.

**Filename parsing**

* [x] movie parser
* [x] TV episode parser
* [x] season detection
* [x] episode range detection
* [ ] anime absolute episode support

**Metadata providers**

* [x] TMDB integration
* [ ] TVDB integration
* [ ] OMDb integration
* [x] provider ID storage

**Matching engine**

* [x] candidate search
* [ ] scoring algorithm
* [ ] confidence thresholds
* [x] auto match
* [ ] unmatched queue

**Metadata storage**

* [x] movies table
* [ ] shows table
* [ ] seasons table
* [x] episodes table
* [x] provider ID mappings

**Artwork**

* [x] poster fetching
* [x] backdrop fetching
* [ ] image caching
* [ ] artwork prioritization

---

# Phase 3 — Web App Media Browser

Goal: basic UI for browsing media.

**Navigation**

* [x] libraries page
* [x] movie grid
* [x] show grid
* [ ] show detail page
* [ ] season page
* [ ] episode page

**Metadata display**

* [x] posters
* [x] descriptions
* [ ] cast lists
* [ ] genres
* [x] runtime

**Search**

* [ ] title search
* [ ] actor search
* [ ] genre filtering

---

# Phase 4 — Playback System

Goal: watch media.

**Playback session API**

* [ ] playback session creation
* [x] playback permissions
* [ ] session tracking

**Video streaming**

* [x] direct play
* [ ] HLS streaming
* [x] transcoding pipeline
* [ ] bitrate profiles

**Custom video player**

* [x] custom controls
* [x] subtitle selection
* [ ] audio track selection
* [x] fullscreen
* [ ] keyboard shortcuts

---

# Phase 5 — User System

Goal: multi-user support.

**Accounts**

* [x] user accounts
* [x] authentication
* [x] session tokens

**Profiles**

* [ ] user profiles
* [ ] profile switching
* [ ] avatar system

**Permissions**

* [x] admin vs user roles
* [ ] library restrictions
* [ ] parental controls

---

# Phase 6 — Watch State

Goal: track viewing behavior.

**Progress tracking**

* [ ] resume position
* [ ] watched status
* [ ] watch history

**Discovery features**

* [ ] continue watching
* [ ] next up episodes
* [ ] recently added

---

# Phase 7 — Media Server Features

Goal: parity with typical media servers.

**Playback features**

* [ ] intro detection
* [ ] skip intro
* [ ] skip credits
* [ ] next episode autoplay

**Subtitles**

* [ ] subtitle downloading
* [ ] subtitle sync adjustment
* [ ] subtitle burn-in

**Transcoding**

* [ ] GPU acceleration
* [ ] codec compatibility
* [ ] subtitle burn-in support

---

# Phase 8 — Devices & Remote Streaming

Goal: use Plum outside the web browser.

**Device support**

* [ ] device registration
* [ ] device auth tokens

**Remote streaming**

* [ ] secure remote access
* [ ] bitrate limits
* [ ] adaptive streaming

**Casting**

* [ ] Chromecast
* [ ] AirPlay

---

# Phase 9 — Admin System

Goal: make the server manageable.

**Dashboard**

* [ ] active sessions
* [ ] server stats
* [ ] scan progress

**Media management**

* [ ] fix match
* [ ] refresh metadata
* [ ] manual identify

**System tools**

* [ ] log viewer
* [ ] job queue viewer
* [ ] database health tools

---

# Phase 10 — Advanced Media Features

Goal: reach Plex/Emby territory.

**Live TV**

* [ ] tuner support
* [ ] channel scanning
* [ ] program guide

**DVR**

* [ ] recording schedules
* [ ] series recording
* [ ] recording management

**Downloads**

* [ ] mobile downloads
* [ ] offline sync

---

# Phase 11 — Ecosystem

Goal: expand beyond core media.

**Plugin system**

* [ ] plugin API
* [ ] plugin lifecycle
* [ ] metadata provider plugins

**Additional libraries**

* [ ] music support
* [ ] photo libraries

**Social features**

* [ ] watch together
* [ ] activity feed

---

# Phase 12 — Polishing

Goal: make the system feel professional.

**Performance**

* [ ] metadata caching
* [ ] query optimization
* [ ] thumbnail pre-generation

**Reliability**

* [ ] job retry system
* [ ] crash recovery
* [ ] backup tools

**UX improvements**

* [ ] keyboard navigation
* [ ] TV remote navigation
* [ ] loading states

---

# A Realistic Development Order

If you're one developer, focus on this path:

1. foundations
2. file scanning
3. metadata matching
4. media browser UI
5. playback
6. users
7. watch tracking
8. admin tools
9. transcoding improvements
10. devices / apps

Everything else is bonus.

---

# My Biggest Advice

Do **not** start with the cool stuff like:

* skip intro
* watch together
* DVR
* mobile downloads

The thing that makes media servers good is:

**files → metadata → playback → progress → reliability**

If those four pillars work perfectly, the rest becomes easy.

---

If you want, I can also make you a **much more detailed "Plum v1 milestone roadmap"** (about 60 tasks) that would realistically get you to a **first usable media server in ~6–8 weeks of development**.
