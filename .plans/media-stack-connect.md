Yes. The clean way to do this is:

**Plum should become the orchestration UI, not the thing that replaces Sonarr/Radarr’s acquisition logic.**

That is the smartest architecture.

Seerr already proves the general pattern: one app handles **discovery, requests, library awareness, Radarr/Sonarr integration, activity monitoring, multiple Radarr/Sonarr servers, and existing-library scans**. It also lets you set defaults like server, root folder, quality profile, and optional automatic search. ([Seerr][1])

## The architecture I’d use

Split Plum into 5 subsystems:

**1. Discovery**

* browse hot/trending/new movies, shows, anime
* search titles
* show “already in library / already requested / downloading / available”

**2. Request & acquisition orchestration**

* when user clicks Add, Plum decides:

  * movie → Radarr
  * show/anime → Sonarr
* Plum sends the title to the correct app with the right profile/root-folder/season selection

**3. Download visibility**

* Plum reads status from Sonarr/Radarr first
* optionally also reads qBittorrent directly for richer torrent progress

**4. Library sync**

* Plum watches your existing library and imported files
* once Sonarr/Radarr import finishes, Plum scans and adds the media automatically

**5. Playback**

* Plum streams the finished media from your library

That keeps responsibilities sane:

* **Sonarr/Radarr** stay responsible for search, grab, import, rename, organize
* **qBittorrent** stays responsible for torrent transfer
* **Plum** becomes the single front door and playback app

## The biggest design decision

You have two possible paths:

### Path A — Plum directly talks to indexers and qBittorrent

This sounds powerful, but it is the wrong starting point.

You would be rebuilding a lot of:

* release search logic
* quality profile logic
* root-folder logic
* import rules
* naming rules
* queue/history state
* failed download handling

That is a huge amount of duplicated complexity.

### Path B — Plum sends requests to Radarr/Sonarr

This is the one I recommend.

Radarr and Sonarr already exist specifically to:

* manage movie/series additions
* track monitored items
* manage profiles and root folders
* connect to download clients
* import completed media

Seerr’s own docs are built around this exact model, including approval flows, multiple servers, activity monitoring, quality profiles, root folders, season selection, and automatic search. ([Seerr][1])

**So Plum should orchestrate, not replace.**

## The user flow you want

Here is the exact flow I’d build.

### 1. Discovery page

Plum has:

* Trending
* Popular
* New releases
* Upcoming
* Anime
* Search

This page should combine:

* external metadata/discovery providers
* your existing library state
* Sonarr/Radarr request state

Each result card should show:

* In library
* Requested
* Downloading
* Missing
* Available to add

That is the “everything in one place” feeling you want.

### 2. Detail page

When user opens a title:

* show synopsis, cast, trailers, posters
* show whether it already exists in Plum
* show whether Sonarr/Radarr already has it monitored
* show whether it is currently downloading/importing

If it is not added yet:

* **movie** → Add to Radarr
* **show/anime** → Add to Sonarr

Advanced options:

* quality profile
* root folder
* monitored on/off
* search now
* for TV: all seasons vs selected seasons

This maps well to how Seerr exposes advanced request options. ([Seerr][1])

### 3. Acquisition status page

Plum shows:

* wanted / pending
* grabbed
* downloading
* importing
* available
* failed

Source of truth:

* mostly Sonarr/Radarr
* qBittorrent for deep transfer stats

### 4. Completed import

Once Sonarr/Radarr finishes import:

* Plum detects the imported file
* refreshes metadata
* makes the item playable
* flips state from downloading → available

## How Plum should integrate technically

## A. Discovery data

Use metadata/discovery providers for search and “what’s hot.”

Good sources:

* TMDB for movies and TV
* anime provider later if needed
* optional Trakt-style trending/recommendation data later

Plum should own this discovery layer.

This is not Sonarr/Radarr’s job.

## B. Radarr/Sonarr integration

Plum should store integration configs for:

* base URL
* API key
* external URL
* default server flag
* quality profile mappings
* root folder mappings
* 4K/non-4K mappings
* anime server flag if you split that

Seerr explicitly supports multiple Radarr/Sonarr servers, separate 4K servers, default servers, root folders, profiles, library scans, and optional automatic search. ([Seerr][1])

So yes, you can absolutely make Plum behave like a more integrated Seerr-style front end.

## C. qBittorrent integration

qBittorrent’s WebUI API supports:

* authentication
* transfer info
* torrent list
* add new torrent
* categories
* pause/resume/delete and other torrent-management actions. ([GitHub][2])

For your app, I would use qBittorrent integration mainly for:

* listing active downloads
* showing progress, speed, ETA
* optionally linking a torrent to a Sonarr/Radarr request
* maybe pause/resume/delete later

I would **not** make qBittorrent the primary source of truth for media workflow.

Primary source of truth should be:

* **Sonarr/Radarr for media lifecycle**
* **qBittorrent for transfer telemetry**

That is cleaner.

## Best workflow design

This is the workflow I’d ship:

### Add movie

1. User searches in Plum
2. User opens movie detail
3. User clicks Add
4. Plum sends the movie to Radarr with chosen settings
5. Radarr searches and sends to qBittorrent
6. Plum polls Radarr status and optionally qB status
7. Radarr imports finished file
8. Plum scans library and marks playable

### Add show/anime

1. User searches in Plum
2. User opens show detail
3. User selects seasons and options
4. Plum sends series to Sonarr
5. Sonarr searches/grabs via qBittorrent
6. Plum shows queue/progress/import state
7. Sonarr imports episodes
8. Plum adds episodes to playable library

That is exactly the shape you want.

## The states Plum should track

For every title, keep a normalized state machine like:

* `not_requested`
* `requested`
* `queued`
* `searching`
* `downloading`
* `importing`
* `available`
* `failed`

Do **not** expose raw Sonarr/Radarr/qBittorrent states directly to your UI everywhere.
Map them into your own simpler status layer.

That will save you a lot of front-end pain.

## The database model you need

You’ll want these tables:

### Integrations

* `integration_servers`
* `integration_credentials`
* `integration_mappings`

### Requests

* `media_requests`
* `request_targets`
  (radarr vs sonarr, server id, profile id, root folder id)

### Jobs/status

* `acquisition_jobs`
* `download_snapshots`
* `import_events`

### Library links

* `media_items`
* `provider_ids`
* `request_media_links`

### External state cache

* `external_media_cache`
* `external_queue_cache`

The point is:

* Plum keeps its own stable view
* Sonarr/Radarr/qBittorrent remain external systems
* Plum syncs their state into its DB

## The feature split I recommend

## V1

Build this first:

* discovery search
* title detail pages
* Radarr add movie
* Sonarr add series
* request status page
* basic qBittorrent progress view
* auto library rescan after import

That already gives you a huge win.

## V1.5

Then add:

* trending / popular pages
* season picker for shows
* profile/root-folder picker
* already-in-library detection
* already-in-sonarr/radarr detection
* external links to Sonarr/Radarr pages

## V2

Then add:

* multi-server support
* separate anime Sonarr
* 4K/non-4K server routing
* notifications
* retry failed requests
* pause/resume/remove torrent controls
* request history and audit log

## What not to do at first

Do **not** try to build:

* your own release search engine
* your own torrent/indexer logic
* your own importer/renamer
* your own full replacement for Arr apps

That is how this project becomes a swamp.

Let the Arr stack do the ugly operational work.

## My blunt recommendation

Build Plum as:

**Discovery + request UI + activity dashboard + playback app**

with these integrations:

* **TMDB / discovery sources** for browsing/search
* **Radarr** for movies
* **Sonarr** for shows/anime
* **qBittorrent** for transfer visibility
* **Plum library scanner/player** for finished media

That gives you the “one place for everything” experience without rebuilding the whole media stack.

## Best next step

I’d design this in three screens first:

* **Discover**
* **Requests / Downloads**
* **Library / Player**

If you want, I can turn this into a concrete **epic-by-epic roadmap** with backend services, DB tables, and API endpoints for Plum.

[1]: https://overseerr.dev/ "Seerr"
[2]: https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-%28qBittorrent-4.1%29 "WebUI API (qBittorrent 4.1) · qbittorrent/qBittorrent Wiki · GitHub"
