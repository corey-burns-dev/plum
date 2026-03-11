import type { MediaItem } from "@/api";

export function clampProgressPercent(value?: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.max(0, Math.min(100, value ?? 0));
}

export function shouldShowProgress(item: Pick<MediaItem, "progress_percent" | "completed">): boolean {
  const percent = clampProgressPercent(item.progress_percent);
  return !item.completed && percent > 0 && percent < 95;
}

export function formatRemainingTime(seconds?: number): string {
  if (!Number.isFinite(seconds) || !seconds || seconds <= 0) return "";
  const wholeSeconds = Math.floor(seconds);
  const hours = Math.floor(wholeSeconds / 3600);
  const minutes = Math.floor((wholeSeconds % 3600) / 60);
  if (hours > 0) {
    return `${hours}h ${String(minutes).padStart(2, "0")}m left`;
  }
  if (minutes > 0) {
    return `${minutes}m left`;
  }
  return `${wholeSeconds}s left`;
}

export function formatEpisodeLabel(item: Pick<MediaItem, "season" | "episode">): string {
  const season = item.season ?? 0;
  const episode = item.episode ?? 0;
  if (season <= 0 && episode <= 0) return "";
  return `S${String(season).padStart(2, "0")}E${String(episode).padStart(2, "0")}`;
}
