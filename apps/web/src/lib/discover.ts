import type {
  DiscoverItem,
  DiscoverLibraryMatch,
  DiscoverMediaType,
  DiscoverTitleDetails,
  DiscoverTitleVideo,
} from "@/api";

type DiscoverDateSource = Pick<DiscoverItem, "release_date" | "first_air_date">;

export function discoverMediaLabel(mediaType: DiscoverMediaType): string {
  return mediaType === "movie" ? "Movie" : "TV";
}

export function discoverPrimaryDate(item: DiscoverDateSource): string {
  return item.release_date || item.first_air_date || "";
}

export function discoverYear(item: DiscoverDateSource): string {
  return discoverPrimaryDate(item).split("-")[0] || "";
}

export function discoverLibraryHref(match: DiscoverLibraryMatch): string {
  if (match.kind === "show" && match.show_key) {
    return `/library/${match.library_id}/show/${match.show_key}`;
  }
  return `/library/${match.library_id}`;
}

export function firstDiscoverMatch(
  matches?: DiscoverLibraryMatch[],
): DiscoverLibraryMatch | undefined {
  return matches?.[0];
}

export function discoverDetailMeta(details: DiscoverTitleDetails): string[] {
  const meta: string[] = [];
  const year = discoverYear(details);
  if (year) {
    meta.push(year);
  }
  if (details.status) {
    meta.push(details.status);
  }
  if (details.media_type === "movie" && details.runtime) {
    meta.push(`${details.runtime} min`);
  }
  if (details.media_type === "tv") {
    if (details.number_of_seasons) {
      meta.push(
        `${details.number_of_seasons} season${details.number_of_seasons === 1 ? "" : "s"}`,
      );
    }
    if (details.runtime) {
      meta.push(`${details.runtime} min episodes`);
    }
  }
  return meta;
}

export function discoverVideoUrl(video: DiscoverTitleVideo): string {
  if (video.site === "YouTube") {
    return `https://www.youtube.com/watch?v=${video.key}`;
  }
  if (video.site === "Vimeo") {
    return `https://vimeo.com/${video.key}`;
  }
  return "";
}
