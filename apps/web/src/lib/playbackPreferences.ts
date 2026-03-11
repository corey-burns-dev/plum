import type { Library } from "../api";

export type SubtitleSize = "small" | "medium" | "large";
export type SubtitlePosition = "top" | "bottom";

export type SubtitleAppearance = {
  size: SubtitleSize;
  position: SubtitlePosition;
  color: string;
};

export type ResolvedLibraryPlaybackPreferences = {
  preferredAudioLanguage: string;
  preferredSubtitleLanguage: string;
  subtitlesEnabledByDefault: boolean;
};

export const subtitleAppearanceStorageKey = "plum:subtitle-appearance";

export const defaultSubtitleAppearance: SubtitleAppearance = {
  size: "medium",
  position: "bottom",
  color: "#ffffff",
};

export const subtitleSizeOptions: Array<{ value: SubtitleSize; label: string }> = [
  { value: "small", label: "Small" },
  { value: "medium", label: "Medium" },
  { value: "large", label: "Large" },
];

export const subtitlePositionOptions: Array<{ value: SubtitlePosition; label: string }> = [
  { value: "bottom", label: "Bottom" },
  { value: "top", label: "Top" },
];

export const languagePreferenceOptions: Array<{ value: string; label: string }> = [
  { value: "en", label: "English" },
  { value: "ja", label: "Japanese" },
  { value: "es", label: "Spanish" },
  { value: "fr", label: "French" },
  { value: "de", label: "German" },
  { value: "it", label: "Italian" },
  { value: "pt", label: "Portuguese" },
  { value: "ko", label: "Korean" },
  { value: "zh", label: "Chinese" },
];

const languageAliases = new Map<string, string>([
  ["en", "en"],
  ["eng", "en"],
  ["english", "en"],
  ["english (us)", "en"],
  ["english us", "en"],
  ["english (uk)", "en"],
  ["ja", "ja"],
  ["jp", "ja"],
  ["jpn", "ja"],
  ["japanese", "ja"],
  ["es", "es"],
  ["spa", "es"],
  ["spanish", "es"],
  ["fr", "fr"],
  ["fre", "fr"],
  ["fra", "fr"],
  ["french", "fr"],
  ["de", "de"],
  ["deu", "de"],
  ["ger", "de"],
  ["german", "de"],
  ["it", "it"],
  ["ita", "it"],
  ["italian", "it"],
  ["pt", "pt"],
  ["por", "pt"],
  ["portuguese", "pt"],
  ["ko", "ko"],
  ["kor", "ko"],
  ["korean", "ko"],
  ["zh", "zh"],
  ["chi", "zh"],
  ["zho", "zh"],
  ["chinese", "zh"],
]);

function defaultLibraryPreferencesForType(type: Library["type"] | undefined): ResolvedLibraryPlaybackPreferences {
  if (type === "anime") {
    return {
      preferredAudioLanguage: "ja",
      preferredSubtitleLanguage: "en",
      subtitlesEnabledByDefault: true,
    };
  }
  if (type === "movie" || type === "tv") {
    return {
      preferredAudioLanguage: "en",
      preferredSubtitleLanguage: "en",
      subtitlesEnabledByDefault: true,
    };
  }
  return {
    preferredAudioLanguage: "",
    preferredSubtitleLanguage: "",
    subtitlesEnabledByDefault: false,
  };
}

export function normalizeLanguagePreference(value: string | undefined | null): string {
  const normalized = value?.trim().toLowerCase() ?? "";
  if (!normalized) return "";
  return languageAliases.get(normalized) ?? normalized.split(/[\s_-]/)[0] ?? normalized;
}

export function languageMatchesPreference(
  value: string | undefined | null,
  preferredLanguage: string | undefined | null,
): boolean {
  const normalizedValue = normalizeLanguagePreference(value);
  const normalizedPreferred = normalizeLanguagePreference(preferredLanguage);
  if (!normalizedValue || !normalizedPreferred) return false;
  return normalizedValue === normalizedPreferred;
}

export function resolveLibraryPlaybackPreferences(
  library: Pick<
    Library,
    | "type"
    | "preferred_audio_language"
    | "preferred_subtitle_language"
    | "subtitles_enabled_by_default"
  > | null
    | undefined,
): ResolvedLibraryPlaybackPreferences {
  const defaults = defaultLibraryPreferencesForType(library?.type);
  return {
    preferredAudioLanguage: normalizeLanguagePreference(
      library?.preferred_audio_language ?? defaults.preferredAudioLanguage,
    ),
    preferredSubtitleLanguage: normalizeLanguagePreference(
      library?.preferred_subtitle_language ?? defaults.preferredSubtitleLanguage,
    ),
    subtitlesEnabledByDefault:
      library?.subtitles_enabled_by_default ?? defaults.subtitlesEnabledByDefault,
  };
}

export function readStoredSubtitleAppearance(): SubtitleAppearance {
  if (typeof window === "undefined") return defaultSubtitleAppearance;
  try {
    const raw = window.localStorage.getItem(subtitleAppearanceStorageKey);
    if (!raw) return defaultSubtitleAppearance;
    const parsed = JSON.parse(raw) as Partial<SubtitleAppearance>;
    const size = parsed.size === "small" || parsed.size === "large" ? parsed.size : "medium";
    const position = parsed.position === "top" ? "top" : "bottom";
    const color = typeof parsed.color === "string" && parsed.color.trim() ? parsed.color : "#ffffff";
    return { size, position, color };
  } catch {
    return defaultSubtitleAppearance;
  }
}

export function writeStoredSubtitleAppearance(preferences: SubtitleAppearance) {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(subtitleAppearanceStorageKey, JSON.stringify(preferences));
}

export function subtitleFontSizeValue(size: SubtitleSize): string {
  switch (size) {
    case "small":
      return "1.1rem";
    case "large":
      return "1.95rem";
    default:
      return "1.45rem";
  }
}
