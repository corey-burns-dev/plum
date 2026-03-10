Yeah — and one important caveat:

**Plex and Jellyfin do not publicly document a full exact scoring formula with weights.**
So the best you can do is infer the matching flow from their docs and design a **practical heuristic scorer** that behaves the same way. Their docs do make clear that matching quality depends heavily on naming/organization, scanners feeding agents/providers, provider IDs in filenames/folders, local metadata, and manual match hints. ([Plex Support][1])

## The mental model

Think of the matcher as a pipeline:

1. **scan files**
2. **parse obvious hints from names/folders**
3. **probe media**
4. **query providers**
5. **score candidate matches**
6. **pick the winner if confidence is high**
7. **fall back to manual hints / IDs / NFO when confidence is low**

That general flow lines up with Plex’s documented separation between **scanners** and **metadata agents**, its scan-then-match process, and Jellyfin’s use of external providers, local NFO, and explicit provider identifiers in names. ([Plex Support][2])

## What they clearly rely on

From the docs, the biggest inputs are:

* **clean folder/file naming**
* **separate library types** like Movies vs TV
* **year**
* **season/episode tokens**
* **provider IDs** in names when available
* **local metadata files**
* **manual hints / fix match tools**

Plex explicitly says naming and organizing media correctly is key to accurate matching, and even provides `.plexmatch` hint files for overrides. Jellyfin likewise says provider IDs in names improve reliability and supports local `.nfo` metadata. ([Plex Support][3])

## A practical scoring algorithm for Plum

Here’s the version I’d build.

### Stage 1: classify the item

Before matching, classify the file as:

* movie
* TV episode
* season pack
* extra / trailer / sample
* unknown

This should come mostly from **library type + path structure + filename tokens**. Plex’s docs stress separating content into major library types because scanners/agents work best that way. ([Plex Support][3])

### Stage 2: extract hints

Parse these fields from filename and folders:

* normalized title
* year
* season number
* episode number
* episode range
* absolute episode number for anime
* edition tags
* source/resolution/group tags
* explicit provider IDs like `tmdbid`, `tvdbid`, `imdbid`

Jellyfin explicitly supports metadata provider identifiers in folder/file names, and Plex supports explicit match hinting through `.plexmatch`. ([Jellyfin][4])

### Stage 3: hard overrides first

Before fuzzy matching, check for:

* explicit provider ID in name
* local `.nfo`
* `.plexmatch`-style folder hints if you add your own equivalent
* existing stored mapping from previous successful match

If any of these exist, they should outrank fuzzy title search almost every time. Jellyfin says identifiers greatly improve identification accuracy, and it supports reading/writing local NFO metadata; Plex’s match hints override filenames and directories. ([Jellyfin][4])

## Suggested score weights

Here’s a strong starting scorer for **TV episodes**:

```text
+1000 explicit provider ID match
+700 local NFO/provider ID match
+400 exact series title normalized match
+250 strong alias/alt-title match
+180 year match
+220 season number match
+260 episode number match
+120 episode title similarity
+80 path structure match (Show/Season/Episode)
+60 runtime near expected runtime
+40 air-date match
-250 title conflict
-300 season mismatch
-350 episode mismatch
-120 year conflict
-100 suspiciously generic title
```

And for **movies**:

```text
+1000 explicit provider ID match
+700 local NFO/provider ID match
+450 exact title normalized match
+260 alias/title similarity match
+220 year exact match
+120 runtime near expected runtime
+80 collection/franchise clue
+60 cast/director clue if you have it
-300 year conflict
-250 title conflict
-120 runtime conflict
-100 low-confidence generic title
```

These numbers are **my recommended design**, not published Plex/Jellyfin weights. But they reflect the priorities their docs strongly imply: explicit IDs and structured hints beat fuzzy matching. ([Jellyfin][4])

## Confidence thresholds

Use thresholds so the system behaves intelligently:

* **900+** → auto-match immediately
* **700–899** → auto-match, but mark as “high confidence”
* **500–699** → tentative; maybe auto-match only if no close competitor
* **below 500** → leave unmatched and ask user/admin to confirm

Also require a **margin** between the top two candidates, for example:

* if top score is only 40 points above second place, do **not** auto-match

That one rule saves a ton of bad matches.

## Candidate generation

Do not score every record in the universe. First generate a small candidate set:

### For TV

Search providers using:

* parsed series title
* optional year
* optional season/episode
* optional absolute episode number
* optional air date

Then merge candidates from:

* TMDB
* TVDB
* OMDb/IMDb if needed

### For movies

Search with:

* normalized title
* year
* edition stripped out
* aliases / punctuation variants

Then de-duplicate by provider IDs.

## Title normalization rules

This matters more than people think.

Normalize by:

* lowercase
* remove punctuation
* collapse whitespace
* strip release-group noise
* convert `and` / `&`
* ignore tags like `1080p`, `WEB-DL`, `BluRay`, `x264`
* keep meaningful pieces like year, season, episode

Examples:

```text
The.Office.US.S02E03.1080p.WEB-DL
-> title: "the office us", season: 2, episode: 3

Blade.Runner.Final.Cut.2007.1080p.BluRay
-> title: "blade runner", edition: "final cut", year: 2007
```

## TVDB’s role in the score

Since you’re using TVDB, I’d use it like this:

### TVDB should be strong for

* TV series identity
* season/episode numbering validation
* alternate order handling
* anime and edge-case series
* episode title confirmation

### TMDB should be strong for

* movie identity
* artwork
* broad search usability
* cast/backdrops/posters

So for TV matching, a good rule is:

* use **TMDB and TVDB as co-primary candidates**
* let **TVDB win ties** when season/episode numbering is clearer
* let **TMDB win artwork selection** unless the user prefers TVDB

That is a much better design than “TMDB first, TVDB only if TMDB fails.”

## Runtime and probe validation

Plex documents that files are analyzed for media properties like codec, resolution, and bitrate during scanning. Use that same idea to validate matches after candidate search. ([Plex Support][1])

For Plum, use `ffprobe` to capture:

* runtime
* resolution
* audio tracks
* subtitle tracks
* container

Then compare against provider metadata where useful:

* TV episode runtime within tolerance
* movie runtime within tolerance
* suspicious mismatches reduce confidence

Do not overweight runtime, because many providers have rough runtimes.

## Local metadata should outrank the internet

Jellyfin supports local `.nfo` metadata, and Plex supports local media assets and explicit hinting. So Plum should treat local metadata as a first-class authority when the user enables it. ([Jellyfin][5])

Best rule:

* **explicit local IDs > local NFO > provider search > fuzzy guess**

That gives power users deterministic control.

## The algorithm in pseudo-flow

```text
for each new file:
  classify item type
  parse filename/folder hints
  probe media with ffprobe

  if explicit provider ID exists:
      fetch exact record
      validate lightly
      match

  else if local NFO exists:
      parse NFO
      fetch referenced provider records if needed
      match

  else:
      generate candidates from TMDB + TVDB (+ fallbacks)
      score each candidate
      sort by score

      if top score >= threshold and top-second >= margin:
          auto-match
      else:
          leave unmatched / request confirmation
```

## What makes a matcher feel “smart”

Three things matter most:

**1. It avoids false positives.**
A missed match is annoying. A wrong match is much worse.

**2. It remembers corrections.**
If the user fixes a match once, store that mapping and reuse it next scan.

**3. It gives manual control.**
Plex has Fix Match and `.plexmatch`; Jellyfin supports identifiers and NFO. Plum should have the same escape hatches. ([Plex Support][6])

## My blunt recommendation for Plum

Build the matcher in this priority order:

1. **filename parser**
2. **provider ID parser**
3. **TMDB + TVDB candidate merge**
4. **weighted scorer**
5. **confidence threshold + margin**
6. **manual fix-match UI**
7. **local NFO support**
8. **saved correction mappings**
9. **OpenSubtitles hash fallback**
10. **anime-specific ordering logic**

That will get you surprisingly close to the “it just works” feel people expect.

I can turn this into a concrete **Go package design** next, with structs, interfaces, and scoring code layout.

[1]: https://support.plex.tv/articles/200889878-matching-process/?utm_source=chatgpt.com "Matching Process | Plex Support"
[2]: https://support.plex.tv/articles/200241558-agents/?utm_source=chatgpt.com "Metadata Agents"
[3]: https://support.plex.tv/articles/naming-and-organizing-your-tv-show-files/?utm_source=chatgpt.com "Naming and Organizing Your TV Show Files"
[4]: https://jellyfin.org/docs/general/server/metadata/identifiers/?utm_source=chatgpt.com "Metadata Provider Identifiers"
[5]: https://jellyfin.org/docs/general/server/metadata/nfo/?utm_source=chatgpt.com "Local .nfo metadata"
[6]: https://support.plex.tv/articles/201018497-fix-match-match/?utm_source=chatgpt.com "Fix Match / Match | Plex Support"
