import Hls from "hls.js";
import type { ClientPlaybackCapabilities, MediaItem } from "../../api";
import { languageMatchesPreference, type SubtitleAppearance } from "../playbackPreferences";

export type BrowserAudioTrack = {
  enabled: boolean;
};

export type BrowserAudioTrackList = {
  length: number;
  [index: number]: BrowserAudioTrack | undefined;
};

export type HlsErrorData = {
  fatal: boolean;
  type?: string;
  details?: string;
  error?: Error;
};

type ParsedVttCueBlock = {
  startTime: number;
  endTime: number;
  text: string;
  settings: string[];
};

export type TrackMenuOption = {
  key: string;
  label: string;
};

export type SubtitleTrackOption = TrackMenuOption & {
  src: string;
  srcLang: string;
};

export type AudioTrackOption = TrackMenuOption & {
  streamIndex: number;
  language: string;
};

function canPlay(element: HTMLVideoElement, value: string): boolean {
  const result = element.canPlayType(value);
  return result === "probably" || result === "maybe";
}

export function detectClientPlaybackCapabilities(): ClientPlaybackCapabilities {
  if (typeof document === "undefined") {
    return {
      supportsNativeHls: false,
      supportsMseHls: false,
      videoCodecs: [],
      audioCodecs: [],
      containers: [],
    };
  }

  const video = document.createElement("video");
  const containers = [
    canPlay(video, "video/mp4") ? "mp4" : null,
    canPlay(video, "video/webm") ? "webm" : null,
    canPlay(video, "video/ogg") ? "ogg" : null,
  ].filter((value): value is string => value != null);

  const videoCodecs = [
    canPlay(video, 'video/mp4; codecs="avc1.42E01E"') ? "h264" : null,
    canPlay(video, 'video/mp4; codecs="hvc1.1.6.L93.B0"') ? "hevc" : null,
    canPlay(video, 'video/mp4; codecs="av01.0.05M.08"') ? "av1" : null,
    canPlay(video, 'video/webm; codecs="vp9"') ? "vp9" : null,
    canPlay(video, 'video/webm; codecs="vp8"') ? "vp8" : null,
  ].filter((value): value is string => value != null);

  const audioCodecs = [
    canPlay(video, 'audio/mp4; codecs="mp4a.40.2"') ? "aac" : null,
    canPlay(video, 'audio/mpeg; codecs="mp3"') ? "mp3" : null,
    canPlay(video, 'audio/webm; codecs="opus"') ? "opus" : null,
    canPlay(video, 'audio/webm; codecs="vorbis"') ? "vorbis" : null,
    canPlay(video, 'audio/mp4; codecs="ac-3"') ? "ac3" : null,
    canPlay(video, 'audio/mp4; codecs="ec-3"') ? "eac3" : null,
    canPlay(video, 'audio/ogg; codecs="flac"') ? "flac" : null,
  ].filter((value): value is string => value != null);

  return {
    supportsNativeHls: canPlay(video, "application/vnd.apple.mpegurl"),
    supportsMseHls: Hls.isSupported(),
    videoCodecs,
    audioCodecs,
    containers,
  };
}

export function getBrowserAudioTracks(
  element: HTMLVideoElement | null,
): BrowserAudioTrackList | null {
  if (!element) return null;
  const audioTracks = (element as HTMLVideoElement & { audioTracks?: BrowserAudioTrackList })
    .audioTracks;
  return audioTracks && typeof audioTracks.length === "number" ? audioTracks : null;
}

export function formatTrackLabel(
  title: string | undefined,
  language: string | undefined,
  fallback: string,
): string {
  const normalizedTitle = title?.trim();
  const normalizedLanguage = language?.trim();
  if (
    normalizedTitle &&
    normalizedLanguage &&
    normalizedTitle.localeCompare(normalizedLanguage, undefined, { sensitivity: "accent" }) !== 0
  ) {
    return `${normalizedTitle} • ${normalizedLanguage}`;
  }
  return normalizedTitle || normalizedLanguage || fallback;
}

export function formatClock(totalSeconds: number): string {
  if (!Number.isFinite(totalSeconds) || totalSeconds <= 0) return "0:00";
  const wholeSeconds = Math.floor(totalSeconds);
  const hours = Math.floor(wholeSeconds / 3600);
  const minutes = Math.floor((wholeSeconds % 3600) / 60);
  const seconds = wholeSeconds % 60;
  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
  }
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

export function formatHlsErrorMessage(data: HlsErrorData): string {
  return data.details || data.type || data.error?.message || "Playback stream failed";
}

export function resolvedVideoDuration(itemDuration: number, elementDuration: number): number {
  if (Number.isFinite(itemDuration) && itemDuration > 0) {
    return itemDuration;
  }
  return Number.isFinite(elementDuration) && elementDuration > 0 ? elementDuration : 0;
}

export function nudgeVideoIntoBufferedRange(video: HTMLVideoElement | null): boolean {
  if (!video || video.buffered.length === 0) {
    return false;
  }

  const currentTime = Number.isFinite(video.currentTime) ? video.currentTime : 0;
  for (let index = 0; index < video.buffered.length; index += 1) {
    const start = video.buffered.start(index);
    const end = video.buffered.end(index);
    if (currentTime + 0.05 < start) {
      video.currentTime = start + 0.01;
      return true;
    }
    if (currentTime >= start && currentTime <= end) {
      return false;
    }
  }

  const lastIndex = video.buffered.length - 1;
  if (lastIndex >= 0) {
    const start = video.buffered.start(lastIndex);
    if (currentTime < start) {
      video.currentTime = start + 0.01;
      return true;
    }
  }

  return false;
}

export function bufferedRangeStartsNearZero(video: HTMLVideoElement | null): boolean {
  if (!video || video.buffered.length === 0) {
    return false;
  }
  return video.buffered.start(0) <= 0.05;
}

function parseVttTimestamp(value: string): number | null {
  const match = value.trim().match(/^(?:(\d+):)?(\d{2}):(\d{2})\.(\d{3})$/);
  if (!match) {
    return null;
  }

  const hours = Number(match[1] ?? 0);
  const minutes = Number(match[2]);
  const seconds = Number(match[3]);
  const milliseconds = Number(match[4]);
  return hours * 3600 + minutes * 60 + seconds + milliseconds / 1000;
}

export function parseVttCueBlocks(body: string): ParsedVttCueBlock[] {
  const normalized = body.replace(/^\uFEFF/, "").replace(/\r\n?/g, "\n");
  const lines = normalized.split("\n");
  const cues: ParsedVttCueBlock[] = [];

  let index = 0;
  if (lines[0]?.startsWith("WEBVTT")) {
    index += 1;
    while (index < lines.length && lines[index]?.trim() !== "") {
      index += 1;
    }
  }

  while (index < lines.length) {
    while (index < lines.length && lines[index]?.trim() === "") {
      index += 1;
    }
    if (index >= lines.length) {
      break;
    }

    const blockStart = lines[index]?.trim() ?? "";
    if (
      blockStart.startsWith("NOTE") ||
      blockStart.startsWith("STYLE") ||
      blockStart.startsWith("REGION")
    ) {
      while (index < lines.length && lines[index]?.trim() !== "") {
        index += 1;
      }
      continue;
    }

    let timingLine = blockStart;
    if (!timingLine.includes("-->")) {
      index += 1;
      timingLine = lines[index]?.trim() ?? "";
    }
    if (!timingLine.includes("-->")) {
      while (index < lines.length && lines[index]?.trim() !== "") {
        index += 1;
      }
      continue;
    }

    const [startToken, endTokenWithSettings] = timingLine.split("-->");
    const timingParts = endTokenWithSettings.trim().split(/\s+/);
    const startTime = parseVttTimestamp(startToken);
    const endTime = parseVttTimestamp(timingParts[0] ?? "");
    if (startTime == null || endTime == null) {
      while (index < lines.length && lines[index]?.trim() !== "") {
        index += 1;
      }
      continue;
    }

    index += 1;
    const textLines: string[] = [];
    while (index < lines.length && lines[index]?.trim() !== "") {
      textLines.push(lines[index] ?? "");
      index += 1;
    }

    cues.push({
      startTime,
      endTime: Math.max(endTime, startTime + 0.001),
      text: textLines.join("\n"),
      settings: timingParts.slice(1),
    });
  }

  return cues;
}

export function getPreferredSubtitleKey(
  subtitleTracks: SubtitleTrackOption[],
  preferredLanguage: string,
  subtitlesEnabled: boolean,
): string {
  if (!subtitlesEnabled || !preferredLanguage) return "off";
  return (
    subtitleTracks.find(
      (track) =>
        languageMatchesPreference(track.srcLang, preferredLanguage) ||
        languageMatchesPreference(track.label, preferredLanguage),
    )?.key ?? "off"
  );
}

export function getPreferredAudioKey(
  audioTracks: AudioTrackOption[],
  preferredLanguage: string,
): string {
  if (!preferredLanguage) return "";
  return (
    audioTracks.find(
      (track) =>
        languageMatchesPreference(track.language, preferredLanguage) ||
        languageMatchesPreference(track.label, preferredLanguage),
    )?.key ?? ""
  );
}

export function applyCueLineSetting(cue: TextTrackCue, position: SubtitleAppearance["position"]) {
  const cueWithLine = cue as TextTrackCue & { line?: number | string };
  if (!("line" in cueWithLine)) return;
  cueWithLine.line = position === "top" ? 8 : "auto";
}

export function applyVttCueSettings(cue: TextTrackCue, settings: string[]) {
  const vttCue = cue as TextTrackCue & {
    align?: "start" | "center" | "end" | "left" | "right";
    line?: number | string;
    position?: number | string;
    size?: number;
    vertical?: string;
  };

  for (const setting of settings) {
    const [key, value] = setting.split(":", 2);
    if (!key || !value) continue;
    switch (key) {
      case "align":
        if (["start", "center", "end", "left", "right"].includes(value)) {
          vttCue.align = value as "start" | "center" | "end" | "left" | "right";
        }
        break;
      case "line":
        vttCue.line = value === "auto" ? "auto" : Number(value.replace("%", ""));
        break;
      case "position":
        vttCue.position = Number(value.replace("%", ""));
        break;
      case "size":
        vttCue.size = Number(value.replace("%", ""));
        break;
      case "vertical":
        vttCue.vertical = value;
        break;
    }
  }
}

export function clearTextTrackCues(track: TextTrack | null) {
  const cues = track?.cues;
  if (!track || !cues) return;
  while (cues.length > 0) {
    const cue = cues[0];
    if (!cue) break;
    track.removeCue(cue);
  }
}

export function buildSubtitleCues(body: string): TextTrackCue[] {
  const CueConstructor =
    typeof window !== "undefined" ? (window.VTTCue ?? window.TextTrackCue) : undefined;
  if (!CueConstructor) {
    return [];
  }

  return parseVttCueBlocks(body)
    .map((cueBlock) => {
      const cue = new CueConstructor(
        cueBlock.startTime,
        cueBlock.endTime,
        cueBlock.text,
      ) as TextTrackCue;
      applyVttCueSettings(cue, cueBlock.settings);
      return cue;
    })
    .filter(Boolean);
}

export function hasTextTrack(video: HTMLVideoElement, track: TextTrack | null): boolean {
  if (!track) {
    return false;
  }
  for (let index = 0; index < video.textTracks.length; index += 1) {
    if (video.textTracks[index] === track) {
      return true;
    }
  }
  return false;
}

export function getSeasonEpisodeLabel(item: MediaItem): string | null {
  const season = item.season ?? 0;
  const episode = item.episode ?? 0;
  if (season <= 0 && episode <= 0) return null;
  return `S${String(season).padStart(2, "0")}E${String(episode).padStart(2, "0")}`;
}

export function getVideoMetadata(item: MediaItem): string {
  const bits = [item.type === "movie" ? "Movie" : item.type === "anime" ? "Anime" : "TV"];
  const seasonEpisode = getSeasonEpisodeLabel(item);
  const releaseYear =
    item.release_date?.split("-")[0] ||
    (item.type === "movie" ? item.title.match(/\((\d{4})\)$/)?.[1] : undefined);

  if (seasonEpisode) bits.push(seasonEpisode);
  if (releaseYear) bits.push(releaseYear);
  if (item.duration > 0) bits.push(formatClock(item.duration));
  return bits.join(" • ");
}

export function getMusicMetadata(item: MediaItem, queueIndex: number, queueSize: number): string {
  const bits = [item.artist || "Unknown Artist"];
  if (item.album) bits.push(item.album);
  if (queueSize > 0) bits.push(`${queueIndex + 1}/${queueSize}`);
  return bits.join(" • ");
}
