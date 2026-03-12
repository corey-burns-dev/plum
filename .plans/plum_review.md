# 🍑 Plum — Deep Code Review & Development Roadmap
*March 2026 · Go backend · React frontend · SQLite · HLS transcoding*

---

## Executive Summary

Plum is a well-structured early-stage media server with a clear architectural identity. The Go backend is clean and idiomatic, the React frontend is sensibly organized, and the monorepo layout is pragmatic. The core pillars — file scanning, metadata identification, transcoding, and basic playback — are meaningfully in place.

The most critical issues to address before this becomes a reliable daily-driver are: the ad-hoc migration system, the global single-transcode-job lock, the absence of DB transactions in write paths, and the open CORS wildcard in development. None of these are architectural blockers — they are well-defined fixes.

> **Overall quality: well above average for a solo project at this stage.** The code is readable, concerns are separated, and the foundation is strong.

---

## Issues at a Glance

| Severity | Area | Issue | Detail |
|----------|------|-------|--------|
| 🔴 Critical | Database | Ad-hoc ALTER TABLE migrations | Schema evolutions run as fire-and-forget ignoring errors; no migration versioning. Will cause silent data corruption on fresh installs with schema drift. |
| 🔴 Critical | Security | CORS wildcard with credentials | When no Origin header is present, `Access-Control-Allow-Origin` is set to `*` while credentials are enabled. This is a CORS spec violation and a security risk. |
| 🔴 Critical | Transcoding | Single global transcode lock | `activeJobCancel` is a package-level global. Multiple concurrent users will cancel each other's transcode jobs with no user attribution. |
| 🟠 High | Database | No transactions on multi-step writes | Scan inserts category row then `media_global` row in separate statements. A crash between them leaves orphaned rows and broken foreign-key state. |
| 🟠 High | Backend | Playback route handler in main.go | Hundreds of lines of handler logic live inside `buildRouter()` instead of a dedicated handler type. Hard to test and will only grow. |
| 🟠 High | Backend | Subtitle ffmpeg streams stdout to ResponseWriter | `cmd.Stdout = w` runs ffmpeg synchronously. If ffmpeg fails mid-stream, partial data is already written; the HTTP status code cannot be changed. |
| 🟠 High | DB / Scan | No per-file transaction in scan | `HandleScanLibraryWithOptions` mutates DB row-by-row with no surrounding transaction. Partial scans on error leave the library in a mixed state. |
| 🟡 Medium | Database | `SetMaxOpenConns(5)` too low | Concurrent ffprobe-heavy scans plus HTTP requests will queue on the 5-connection pool and time out under load. |
| 🟡 Medium | Security | Session cookie `Secure: false` | In any deployment behind a TLS reverse proxy, the cookie will be sent over plain HTTP if accidentally accessed. |
| 🟡 Medium | Backend | Hardcoded `/tmp` paths for transcoding | `plum_transcoded_{id}.mp4` written to `/tmp` with no cleanup. Old transcodes from previous runs persist and can serve stale content. |
| 🟡 Medium | Backend | `SeedSample` called on every boot | Queries `media_global` on every startup even when the DB has content. The function is also a stub that does nothing. |
| 🟡 Medium | Frontend | WS connection lives in PlayerContext | The WebSocket re-connects on every render cycle if the provider is not at the very root. Also leaks if `PlayerProvider` is remounted. |
| 🟡 Medium | Frontend | `transcodeVersion` as version counter | Used as a side-channel to signal HLS source URL changes. Creates invisible coupling; HLS.js should be restarted directly instead. |
| 🔵 Low | Backend | `mediaTableForKind` double-dispatch | The unexported wrapper just calls the exported version. Unnecessary indirection — consolidate to one function. |
| 🔵 Low | Backend | No structured logging | All logging is `log.Printf` / `log.Fatalf`. No log levels, no structured fields, no request IDs. Hard to filter in production. |
| 🔵 Low | Frontend | `closePlayer` is an alias for `dismissDock` | Undocumented aliasing in context value. Either make it distinct or remove it. |

---

## Backend Deep Dive

### Database & Schema

#### Migration System

The current approach runs `ALTER TABLE` statements in `createSchema()` with errors silently ignored (`_, _ = db.Exec(...)`). This works for a single developer but breaks in several ways:

- A fresh install runs `CREATE TABLE IF NOT EXISTS` with all columns already defined, then the `ALTER TABLE` block tries to add them again — SQLite ignores this with an error that is swallowed.
- There is no migration version tracking, so there is no way to know which state a given DB is in.
- Adding a new migration requires re-running the whole block on every boot, not just on first run.

> **Recommendation:** Replace with an explicit migration table. Track applied migrations by version number. Use `golang-migrate/migrate` or a simple hand-rolled versioned runner.

#### Missing Transactions

`insertScannedItem()` runs two statements: `INSERT INTO {table}` and `INSERT INTO media_global`. If the process crashes between these two, the category row exists but has no `media_global` entry. The item then becomes unreachable by ID but still consumes a row.

Same issue applies to subtitle insertion during scan, thumbnail update, and the admin setup flow (create user + create session in separate statements).

```go
// Fix: wrap in a transaction
tx, err := dbConn.BeginTx(ctx, nil)
defer tx.Rollback()
// ... insert category row with tx.QueryRowContext
// ... insert media_global with tx.QueryRowContext
tx.Commit()
```

#### Connection Pool Sizing

`db.SetMaxOpenConns(5)` is conservative for a scan-heavy workload. With a large library scan running plus a user browsing the UI, 5 connections will queue. For SQLite with WAL mode (which is correctly enabled), a more appropriate setting is `MaxOpenConns(10)` + `MaxIdleConns(5)`, or separate read/write pools.

#### Scan Atomicity

`HandleScanLibraryWithOptions()` walks the filesystem and writes rows one-by-one with no enclosing transaction. If the context is cancelled mid-scan, the library is left partially updated. The `pruneMissingMedia()` call at the end also won't run, so stale entries accumulate.

Consider batching inserts within a transaction, or at minimum running the prune step in a deferred function so it always executes even on error.

---

### HTTP Handlers

#### Handler Logic in main.go

`buildRouter()` contains ~180 lines of inline handler logic for playback sessions, stream serving, subtitle extraction, transcoding, and thumbnail generation. This cannot be unit-tested, mixes routing concerns with business logic, and will only grow.

> **Move all inline handlers into a `PlaybackHandler` struct in `internal/http/` matching the pattern used by `LibraryHandler` and `AuthHandler`.**

#### Subtitle Streaming

`HandleStreamSubtitle()` and `HandleStreamEmbeddedSubtitle()` set `cmd.Stdout = w` and call `cmd.Run()`. This means:

- The HTTP status 200 is sent implicitly when the first byte is written.
- If ffmpeg fails mid-stream, partial VTT data is written to the client and the error cannot be reported via status code.
- There is no context propagation — a client disconnect does not cancel the ffmpeg process.

```go
// Better approach: buffer output
var buf bytes.Buffer
cmd.Stdout = &buf
if err := cmd.Run(); err != nil {
    http.Error(w, "subtitle extraction failed", 500)
    return
}
w.Header().Set("Content-Type", "text/vtt")
w.Write(buf.Bytes())
```

#### CORS Configuration

The CORS middleware has a subtle bug: when there is no `Origin` header, it falls through to set `Access-Control-Allow-Origin: *`. The middleware also sets `Access-Control-Allow-Credentials: true` globally. Combining `*` with credentials is forbidden by the CORS spec and will be rejected by compliant browsers. The dev environment reflects the origin back, meaning any origin can make credentialed requests.

---

### Transcoding

#### Global Job Lock

The `activeJobMu` / `activeJobCancel` / `activeJobID` triplet is a package-level global. This means:

- Only one transcode can run at a time across the entire server process.
- Starting a new transcode for User A cancels the in-progress transcode for User B with no notification.
- The old `/api/transcode/{id}` system coexists with the newer `PlaybackSessionManager` — both are registered. This is inconsistent.

> **The `PlaybackSessionManager` approach (per-session isolation, revision tracking, HLS output) is clearly the right path. The old `HandleStartTranscode` / `HandleCancelTranscode` pair should be removed.**

#### Hardcoded /tmp Paths

`plum_transcoded_{id}.mp4` in `HandleStreamMedia` checks `/tmp` for a transcoded file. This file is never cleaned up. A server restart will serve a stale transcode from a previous run. The newer HLS session system correctly uses a managed temp directory; the old transcode path does not.

---

### WebSocket Hub

The Hub implementation is clean. The broadcast channel buffer of 16 is small but the drop-slow-client behavior is correct. One risk: if the broadcast channel itself is full, messages are silently dropped.

The Hub does not support per-user targeting — all events are broadcast to all clients. For multi-user deployments, session update events will be delivered to all connected clients. This is a privacy concern and a source of race conditions if two users are playing different content.

---

### Auth System

- **Session cookie `Secure: false`** — Set based on an env var (`PLUM_SECURE_COOKIES=true`) rather than hardcoding false.
- **Two DB queries per request** in `AuthMiddleware` (session lookup + user lookup). These could be a single JOIN.
- **No session cleanup job** — Expired sessions accumulate indefinitely. A periodic `DELETE FROM sessions WHERE expires_at < NOW()` is trivial to add alongside the existing `StartIMDbRatingsSync` goroutine pattern.

---

## Frontend Deep Dive

### PlayerContext

#### WebSocket Lifecycle

The WebSocket connection is established inside `PlayerProvider` with a `useEffect`. If `PlayerProvider` is ever remounted, the connection tears down and re-establishes with a 3-second gap. More critically, the WS connection is entangled with player state — closing the player (e.g., user dismisses the dock) should not kill the connection, since WS is also needed for background scan status updates.

> **Move the WS connection to a separate `WsContext` or a top-level singleton outside of `PlayerProvider`.**

#### transcodeVersion Coupling

`transcodeVersion` is used as an implicit signal to force HLS.js to reload the source. Any component reading it must know this convention, which is not obvious from the type. Instead, `videoSourceUrl` should change when the source changes, and the HLS player component should simply call `hls.loadSource()` when it detects the change. `transcodeVersion` becomes unnecessary.

#### Queue Logic

The music queue is well-designed with shuffle/unshuffle preserving the base queue. The video queue doesn't support queuing — each `play()` call creates a fresh single-item queue. `playShowGroup` is architecturally ready for next-episode support since it passes all sorted episodes.

---

### Query Layer

`LIBRARIES_STALE_MS` and `LIBRARY_MEDIA_STALE_MS` are both 60 seconds. During an active scan, the user won't see new media for up to 60 seconds unless they manually refresh. Scan completion WS events are received but don't invalidate the TanStack Query cache.

> **Connect scan completion WS events to `queryClient.invalidateQueries()` so the library refreshes automatically when a scan finishes.**

---

### API Layer

The `@plum/shared` package containing the API client is a clean pattern — types are co-located with the client and exported to both web and desktop apps. The one gap: no retry logic or timeout configuration. For scan operations that can take minutes, a single failed request returns an unrecoverable error.

---

## Performance & Architecture Optimizations

### Database Query Patterns

- `getSubtitlesByMediaIDs` / `getEmbeddedSubtitlesByMediaIDs` build `IN` clauses by string concatenation. For very large libraries, consider batching in chunks of 500.
- `queryAllMediaByKind` runs 4 separate queries and merges in Go. A `UNION ALL` in SQL would be cleaner.
- `ListShowEpisodeRefs` loads all episodes for a library then filters by `showKey` in Go. A SQL `WHERE title LIKE ?` clause would reduce data transfer.

### Scan Performance

- Each file during scan runs ffprobe **twice** — once for duration, once for subtitle/audio streams. These can be combined into a single `ffprobe -show_streams` call.
- Metadata identification (TMDB API calls) is sequential — one HTTP request per file. A worker pool of 4–8 goroutines with rate limiting would dramatically speed up large library scans.

### Transcoding & HLS

- The 250ms polling interval in `runRevision()` to detect `revisionReady()` burns unnecessary CPU. `fsnotify` would be more efficient.
- Segment size is not configurable. Exposing segment duration as a transcoding setting (e.g., 4s for local, 6s for remote) would improve seeking behavior.
- Old revision directories are only cleaned up when the session is closed. With many audio track switches, temp storage can grow unbounded within a session.

### Frontend Performance

- `LibraryPosterGrid` renders all posters at once with no virtualization. For 500+ item libraries this will cause noticeable render pauses. Use `tanstack-virtual` or `react-window`.
- The `useMemo` in `PlayerProvider` has a 28-item dependency array. Split into `PlayerStateContext` and `PlayerActionsContext` to reduce re-renders.
- Volume and muted state stored in `PlayerContext` cause all consumers to re-render on volume change. These should be local state within `PlaybackDock`.

---

## Revised Development Roadmap

The existing `milestones.md` is a solid high-level plan. This re-sequences based on the issues found above with the goal of reaching a stable, daily-usable v1 as quickly as possible.

### P0 — Now: Fix Critical Issues
- Replace ad-hoc `ALTER TABLE` migrations with versioned migration runner
- Wrap `insertScannedItem` in a DB transaction
- Fix CORS wildcard + credentials combination
- Remove deprecated `/api/transcode` routes (use sessions only)
- Add `Secure: true` to session cookie via env flag

**Milestone: No data corruption risk on fresh install**

### P1: Playback Polish
- Move inline playback handlers out of `main.go` into `PlaybackHandler`
- Buffer subtitle ffmpeg output before writing response
- Add context propagation to subtitle extraction (cancel on client disconnect)
- Clean up old `/tmp` transcode files on startup
- Add periodic expired session cleanup

**Milestone: Stable single-user daily driver**

### P2: Watch State
- Resume position: save progress every 10s via `PUT /api/media/{id}/progress`
- Watched status + history UI
- Continue watching row on home dashboard
- Next-episode autoplay (queue already supports it)
- Invalidate TQ cache on scan WS events

**Milestone: Core media server feature parity**

### P3: Multi-user & Admin
- Per-user WS targeting (replace broadcast-all with user-scoped events)
- Active sessions dashboard
- Library-level permissions
- Job queue viewer / log viewer
- Fix match / refresh metadata UI

**Milestone: Shareable with family/friends**

### P4: Scan Performance
- Worker pool for TMDB identification (4–8 goroutines with rate limit)
- Combine ffprobe calls (duration + streams in one pass)
- File watcher for auto-rescan on library change (`fsnotify`)
- Virtual list in `LibraryPosterGrid`

**Milestone: Fast scans for large libraries**

### P5: Advanced Playback
- Subtitle sync adjustment
- Intro detection (via fingerprinting or manual chapter marks)
- GPU acceleration config validation (VAAPI probe on startup)
- Adaptive bitrate HLS (multiple quality levels)
- Chromecast / AirPlay cast support

**Milestone: Plex/Jellyfin feature parity on playback**

### P6: *arr Integration
- Radarr/Sonarr connection config in settings
- Discovery page (TMDB trending/popular/search)
- Request → Radarr/Sonarr → auto-rescan pipeline
- Download queue visibility from qBittorrent
- Notification system

**Milestone: Full Overseerr-style media management**

---

## Quick Wins (< 1 Hour Each)

- Remove the `mediaTableForKind()` unexported wrapper that just calls `MediaTableForKind()`.
- Remove the `closePlayer` alias from `PlayerContext` value, or make it a distinct function.
- Remove the `SeedSample()` call from `main.go` startup (it does nothing and queries the DB on every boot).
- Add `DELETE FROM sessions WHERE expires_at < ?` cleanup to the `IMDbRatingsSync` goroutine.
- Add a request timeout to the HTTP clients in `metadata/tmdb.go`, `metadata/tvdb.go`, etc. (currently unbounded).
- Add an index on `tv_episodes(library_id, season, episode)` for show grouping queries.
- Expose WS reconnect status in the UI (currently `wsConnected` is in context but not surfaced to users).

---

## Note on Sonarr/Radarr Integration (`.plans/super-features.md`)

The super-features plan is architecturally sound. **Path B (Plum orchestrates the *arr apps) is the right call.** Building your own indexer/grabber/importer would be a multi-year distraction.

A few additions worth noting:

- **Use webhooks, not polling.** Sonarr and Radarr both emit `on-grab`, `on-import`, and `on-upgrade` webhook events. This lets Plum react to state changes instantly rather than polling every N seconds. Far cleaner than a polling loop.
- **Cache external state locally.** The state machine (`not_requested → requested → downloading → available`) should be backed by a DB table with a sync timestamp, not inferred from live API calls at render time.
- **Keep qBittorrent integration read-only initially.** Show progress/ETA from qBittorrent without write operations (pause/delete). Read-only is safe and useful on day one; write operations need confirmation UX to avoid accidental removals.

---

## Summary

Plum has a genuinely solid foundation. The architecture is coherent, the Go backend is idiomatic, the frontend is well-organized, and the HLS playback session model is the right design. The critical path to daily usability is fixing the migration system, adding DB transactions to write paths, and cleaning up the playback handler architecture. Everything else is polish on top of a good base.
