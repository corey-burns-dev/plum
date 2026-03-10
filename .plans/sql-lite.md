Yep — and there are some very usable lessons in it.

The big recent change is that **Jellyfin 10.11 moved the old `library.db` into a consolidated `jellyfin.db` and now routes database access through EF Core**, with the old `library.db` backed up as `library.db.old` during migration. ([jellyfin.org][1])

What that tells you right away is this:

**Jellyfin’s database is not modeled like a pure “movie/show/episode only” schema.**
It is closer to a **general media item graph plus related tables for user data, images, metadata, and provider mappings**. You can see that in the migration code and generated queries referencing entities like **`BaseItemEntity`**, **`UserData`**, **`Images`**, and extra/provider-id style data. ([GitHub][2])

## The useful high-level structure to steal

From the public migration notes, code references, and query traces, Jellyfin’s shape looks roughly like this:

* a **core base item table/entity** for most library objects
* **type-based specialization** rather than completely separate isolated trees
* related tables for:

  * user state
  * images/artwork
  * provider IDs / external identifiers
  * library relationships / parent-child relationships
  * playback-related state
  * jobs / background processing
* a lot of metadata historically split between DB and filesystem/XML, now being pushed more into the DB model via EF Core. ([GitHub][3])

That is a strong clue for Plum:
**separate canonical media entities, user state, provider mappings, and assets. Do not flatten everything into one giant media table.**

## What Jellyfin seems to get right

### 1. Separate user state from media identity

The query traces show **`UserData`** associated with media items rather than stuffing things like favorites/progress directly into the core media row. ([GitHub][4])

That is exactly right for your app.

For Plum, keep these separate:

* `media_items`
* `users`
* `user_media_state`

That lets multiple users have:

* different watch progress
* favorites
* watched status
* last played
* ratings

on the same movie or episode.

### 2. Treat artwork as its own concern

Jellyfin queries include **`Images`** separately. ([GitHub][4])

Also right.

For Plum, do **not** put poster/backdrop URLs directly on your main media rows only. Use a related table like:

* `media_images`

  * media_item_id
  * provider
  * image_type
  * url / local path
  * width / height
  * language
  * is_primary
  * vote / score / sort order

This gives you:

* multiple posters
* multiple backdrops
* provider-specific artwork
* local overrides

### 3. Use provider IDs as first-class data

Jellyfin explicitly documents **provider identifiers in file/folder names** to improve matching. ([jellyfin.org][5])

That means Plum should absolutely have a table like:

* `media_provider_ids`

  * media_item_id
  * provider (`tmdb`, `tvdb`, `imdb`, etc.)
  * provider_item_id
  * source (`auto`, `manual`, `local_nfo`)
  * confidence
  * created_at

This is one of the most important things to get right.

### 4. Keep libraries virtual

Jellyfin’s library docs describe libraries as **virtual collections** that can include several filesystem locations. ([jellyfin.org][6])

That is a very good model.

For Plum:

* `libraries`
* `library_paths`
* `media_files`

Do not tie one library to one folder only.

## What I would **not** copy too literally

### 1. A giant generic base-item model for everything

Jellyfin’s generalized `BaseItemEntity` approach is flexible, but it can also become heavy and produce complex queries, especially once everything hangs off one central item model. The public issues around slow big-show pages and EF Core query warnings give you a taste of that complexity. ([GitHub][7])

For Plum, I would split the difference:

Have a **canonical `media_items` table** for shared fields, but still keep dedicated tables for:

* `movies`
* `shows`
* `seasons`
* `episodes`

That gives you cleaner domain logic than “everything is one row with a type.”

### 2. Letting migration complexity pile up

Jellyfin’s 10.11 migration was big enough that they published special migration warnings and backup instructions, and multiple migration issues followed. ([jellyfin.org][1])

Lesson for Plum:

* keep migrations small
* avoid giant one-time refactors if possible
* keep backups easy
* version your metadata and provider cache carefully

### 3. Ignoring SQLite concurrency realities

Jellyfin has had public discussion around SQLite locking and scan-time lag, and they note WAL is used by default now. ([jellyfin.org][8])

That does **not** mean SQLite is bad. It means:

* long write-heavy scans
* too much hot-path session/progress churn
* heavy ORM query graphs

can bite you.

For Plum, if you stay on SQLite early:

* use WAL
* keep write transactions short
* batch scan writes
* avoid chatty progress updates every tiny interval
* index for your actual UI paths

## The schema lessons I’d steal for Plum

Here’s the version I’d actually build around.

### Core content

* `libraries`
* `library_paths`
* `media_items`
* `movies`
* `shows`
* `seasons`
* `episodes`
* `media_files`
* `media_streams`

### Metadata and matching

* `media_provider_ids`
* `provider_metadata_cache`
* `media_images`
* `media_people`
* `people`
* `media_genres`
* `genres`
* `collections`
* `collection_items`

### User-specific state

* `users`
* `user_profiles`
* `user_media_state`
* `watch_history`
* `resume_positions`
* `favorites`

### Playback / server activity

* `playback_sessions`
* `transcode_jobs`
* `scan_jobs`
* `devices`
* `auth_sessions`

That is cleaner than shoving all of it into one abstract item tree.

## The most important design principle

The best lesson from Jellyfin is not a specific table name.

It is this:

**Separate these four things hard:**

1. **logical media identity**
   movie, show, season, episode

2. **physical file reality**
   file path, bitrate, codec, streams, versions

3. **external metadata/provider mappings**
   TMDB, TVDB, IMDb, local NFO, manual fixes

4. **user-specific state**
   watched, progress, favorites, ratings

If you mix those together, the schema gets ugly fast.

## My blunt recommendation for Plum

Copy Jellyfin’s good instincts, not its full shape:

* yes to provider IDs
* yes to separate artwork tables
* yes to separate user data
* yes to virtual libraries
* yes to local/remote metadata layering

But for your own schema:

* keep **typed tables for movie/show/season/episode**
* keep file records separate
* keep provider cache separate
* keep user state separate
* avoid one mega-entity for every possible thing

That will be easier to reason about than a pure “everything is a BaseItem” model.

If you want, I’ll turn this into a **concrete Plum schema draft** with actual table names, columns, and relationships.

[1]: https://jellyfin.org/posts/jellyfin-release-10.11.0/?utm_source=chatgpt.com "Jellyfin 10.11.0"
[2]: https://github.com/jellyfin/jellyfin/blob/master/Jellyfin.Server/Migrations/Routines/MigrateLibraryDb.cs?utm_source=chatgpt.com "jellyfin/Jellyfin.Server/Migrations/Routines/MigrateLibraryDb.cs ..."
[3]: https://github.com/jellyfin/jellyfin-meta/issues/26?utm_source=chatgpt.com "Library database EFCore migration and API v2 discussion"
[4]: https://github.com/jellyfin/jellyfin/issues/16332?utm_source=chatgpt.com "EFCore warning about missing OrderBy · Issue #16332"
[5]: https://jellyfin.org/docs/general/server/metadata/identifiers/?utm_source=chatgpt.com "Metadata Provider Identifiers"
[6]: https://jellyfin.org/docs/general/server/libraries/?utm_source=chatgpt.com "Libraries"
[7]: https://github.com/jellyfin/jellyfin/issues/15917?utm_source=chatgpt.com "Shows with many episodes never load on browser or take ..."
[8]: https://jellyfin.org/posts/SQLite-locking/?utm_source=chatgpt.com "SQLite concurrency and why you should care about it"
