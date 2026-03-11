import { useEffect, useState } from "react";
import type {
  HardwareEncodeFormat,
  Library,
  TranscodingSettings as TranscodingSettingsShape,
  TranscodingSettingsWarning,
  VaapiDecodeCodec,
} from "@plum/contracts";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useAuthState } from "@/contexts/AuthContext";
import {
  languagePreferenceOptions,
  normalizeLanguagePreference,
  resolveLibraryPlaybackPreferences,
} from "@/lib/playbackPreferences";
import {
  useLibraries,
  useTranscodingSettings,
  useUpdateLibraryPlaybackPreferences,
  useUpdateTranscodingSettings,
} from "@/queries";

const decodeCodecOptions: Array<{
  key: VaapiDecodeCodec;
  label: string;
  description: string;
}> = [
  { key: "h264", label: "H.264", description: "Use VAAPI decode for 8-bit AVC video." },
  { key: "hevc", label: "HEVC", description: "Use VAAPI decode for standard HEVC streams." },
  { key: "mpeg2", label: "MPEG-2", description: "Use VAAPI decode for legacy MPEG-2 sources." },
  { key: "vc1", label: "VC-1", description: "Use VAAPI decode for VC-1 content when available." },
  { key: "vp8", label: "VP8", description: "Use VAAPI decode for VP8 sources." },
  { key: "vp9", label: "VP9", description: "Use VAAPI decode for standard VP9 streams." },
  { key: "av1", label: "AV1", description: "Use VAAPI decode for AV1 content." },
  {
    key: "hevc10bit",
    label: "HEVC 10-bit",
    description: "Allow VAAPI decode for 10-bit HEVC video.",
  },
  { key: "vp910bit", label: "VP9 10-bit", description: "Allow VAAPI decode for 10-bit VP9 video." },
];

const encodeFormatOptions: Array<{
  key: HardwareEncodeFormat;
  label: string;
  description: string;
}> = [
  { key: "h264", label: "H.264", description: "Best playback compatibility and safest default." },
  {
    key: "hevc",
    label: "HEVC",
    description: "Smaller output with newer client support requirements.",
  },
  {
    key: "av1",
    label: "AV1",
    description: "Highest efficiency, but hardware support varies widely.",
  },
];

function cloneSettings(settings: TranscodingSettingsShape): TranscodingSettingsShape {
  return {
    ...settings,
    decodeCodecs: { ...settings.decodeCodecs },
    encodeFormats: { ...settings.encodeFormats },
  };
}

type LibraryPlaybackPreferencesForm = {
  preferred_audio_language: string;
  preferred_subtitle_language: string;
  subtitles_enabled_by_default: boolean;
};

function cloneLibraryPlaybackPreferences(library: Library): LibraryPlaybackPreferencesForm {
  const resolved = resolveLibraryPlaybackPreferences(library);
  return {
    preferred_audio_language: normalizeLanguagePreference(resolved.preferredAudioLanguage),
    preferred_subtitle_language: normalizeLanguagePreference(resolved.preferredSubtitleLanguage),
    subtitles_enabled_by_default: resolved.subtitlesEnabledByDefault,
  };
}

function libraryPreferencesEqual(
  left: LibraryPlaybackPreferencesForm,
  right: LibraryPlaybackPreferencesForm,
): boolean {
  return (
    left.preferred_audio_language === right.preferred_audio_language &&
    left.preferred_subtitle_language === right.preferred_subtitle_language &&
    left.subtitles_enabled_by_default === right.subtitles_enabled_by_default
  );
}

export function Settings() {
  const { user } = useAuthState();
  const isAdmin = user?.is_admin ?? false;
  const librariesQuery = useLibraries();
  const settingsQuery = useTranscodingSettings({ enabled: isAdmin });
  const updateLibraryPreferences = useUpdateLibraryPlaybackPreferences();
  const updateSettings = useUpdateTranscodingSettings();
  const [form, setForm] = useState<TranscodingSettingsShape | null>(null);
  const [libraryForms, setLibraryForms] = useState<Record<number, LibraryPlaybackPreferencesForm>>({});
  const [librarySaveMessages, setLibrarySaveMessages] = useState<Record<number, string | null>>({});
  const [savingLibraryId, setSavingLibraryId] = useState<number | null>(null);
  const [warnings, setWarnings] = useState<TranscodingSettingsWarning[]>([]);
  const [saveMessage, setSaveMessage] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    if (!settingsQuery.data || dirty) return;
    setForm(cloneSettings(settingsQuery.data.settings));
    setWarnings(settingsQuery.data.warnings);
  }, [dirty, settingsQuery.data]);

  useEffect(() => {
    if (!librariesQuery.data) return;
    setLibraryForms((current) => {
      const next = { ...current };
      for (const library of librariesQuery.data) {
        const fallback = cloneLibraryPlaybackPreferences(library);
        const existing = current[library.id];
        const currentLibrary = cloneLibraryPlaybackPreferences(library);
        next[library.id] =
          existing && !libraryPreferencesEqual(existing, currentLibrary) ? existing : fallback;
      }
      return next;
    });
  }, [librariesQuery.data]);

  const videoLibraries = (librariesQuery.data ?? []).filter((library) => library.type !== "music");
  const getLibraryFormFallback = (libraryId: number) => {
    const library = librariesQuery.data?.find((item) => item.id === libraryId);
    return library
      ? cloneLibraryPlaybackPreferences(library)
      : {
          preferred_audio_language: "en",
          preferred_subtitle_language: "en",
          subtitles_enabled_by_default: true,
        };
  };

  const setLibraryField = <K extends keyof LibraryPlaybackPreferencesForm>(
    libraryId: number,
    key: K,
    value: LibraryPlaybackPreferencesForm[K],
  ) => {
    setLibraryForms((current) => {
      const base = current[libraryId] ?? getLibraryFormFallback(libraryId);
      return {
        ...current,
        [libraryId]: { ...base, [key]: value },
      };
    });
    setLibrarySaveMessages((current) => ({ ...current, [libraryId]: null }));
  };

  const saveLibraryPreferences = async (library: Library) => {
    const payload = libraryForms[library.id] ?? cloneLibraryPlaybackPreferences(library);
    setSavingLibraryId(library.id);
    setLibrarySaveMessages((current) => ({ ...current, [library.id]: null }));
    try {
      const updated = await updateLibraryPreferences.mutateAsync({ libraryId: library.id, payload });
      setLibraryForms((current) => ({
        ...current,
        [library.id]: cloneLibraryPlaybackPreferences(updated),
      }));
      setLibrarySaveMessages((current) => ({
        ...current,
        [library.id]: "Playback defaults saved.",
      }));
    } catch (error) {
      setLibrarySaveMessages((current) => ({
        ...current,
        [library.id]:
          error instanceof Error ? error.message : "Failed to save playback defaults.",
      }));
    } finally {
      setSavingLibraryId(null);
    }
  };

  const playbackDefaultsSection = (
    <section className="rounded-[var(--radius-lg)] border border-[var(--plum-border)] bg-[var(--plum-panel)]/80 p-6 shadow-[0_20px_45px_rgba(0,0,0,0.35)]">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold text-[var(--plum-text)]">Playback defaults</h1>
        <p className="max-w-2xl text-sm text-[var(--plum-muted)]">
          Choose the default audio and subtitle language for each library. Anime libraries default
          to Japanese audio with English subtitles; TV and movie libraries default to English for
          both when available.
        </p>
      </div>

      {librariesQuery.isLoading ? (
        <p className="mt-5 text-sm text-[var(--plum-muted)]">Loading libraries…</p>
      ) : librariesQuery.isError ? (
        <p className="mt-5 text-sm text-red-300">
          {librariesQuery.error.message || "Failed to load libraries."}
        </p>
      ) : videoLibraries.length === 0 ? (
        <p className="mt-5 text-sm text-[var(--plum-muted)]">
          Add a TV, movie, or anime library to configure playback defaults.
        </p>
      ) : (
        <div className="mt-6 grid gap-4">
          {videoLibraries.map((library) => {
            const current = libraryForms[library.id] ?? cloneLibraryPlaybackPreferences(library);
            const saved = cloneLibraryPlaybackPreferences(library);
            const isDirty = !libraryPreferencesEqual(current, saved);
            const message = librarySaveMessages[library.id];

            return (
              <article
                key={library.id}
                className="rounded-[var(--radius-md)] border border-[var(--plum-border)] bg-[var(--plum-panel-alt)]/60 p-5"
              >
                <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
                  <div>
                    <h2 className="text-lg font-medium text-[var(--plum-text)]">{library.name}</h2>
                    <p className="mt-1 text-xs uppercase tracking-[0.16em] text-[var(--plum-muted)]">
                      {library.type}
                    </p>
                  </div>
                  <Button
                    onClick={() => void saveLibraryPreferences(library)}
                    disabled={!isDirty || savingLibraryId === library.id}
                  >
                    {savingLibraryId === library.id ? "Saving…" : "Save defaults"}
                  </Button>
                </div>

                <div className="mt-5 grid gap-4 md:grid-cols-3">
                  <div>
                    <label
                      className="mb-2 block text-sm font-medium text-[var(--plum-text)]"
                      htmlFor={`library-audio-${library.id}`}
                    >
                      Preferred audio
                    </label>
                    <select
                      id={`library-audio-${library.id}`}
                      value={current.preferred_audio_language}
                      onChange={(event) =>
                        setLibraryField(
                          library.id,
                          "preferred_audio_language",
                          normalizeLanguagePreference(event.target.value),
                        )
                      }
                      className="flex h-9 w-full rounded-[var(--radius-md)] border border-[var(--plum-border)] bg-[var(--plum-panel)] px-3 py-1 text-sm text-[var(--plum-text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--plum-ring)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--plum-bg)]"
                    >
                      {languagePreferenceOptions.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  </div>

                  <div>
                    <label
                      className="mb-2 block text-sm font-medium text-[var(--plum-text)]"
                      htmlFor={`library-subtitles-${library.id}`}
                    >
                      Preferred subtitles
                    </label>
                    <select
                      id={`library-subtitles-${library.id}`}
                      value={current.preferred_subtitle_language}
                      onChange={(event) =>
                        setLibraryField(
                          library.id,
                          "preferred_subtitle_language",
                          normalizeLanguagePreference(event.target.value),
                        )
                      }
                      className="flex h-9 w-full rounded-[var(--radius-md)] border border-[var(--plum-border)] bg-[var(--plum-panel)] px-3 py-1 text-sm text-[var(--plum-text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--plum-ring)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--plum-bg)]"
                    >
                      {languagePreferenceOptions.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  </div>

                  <div className="flex items-end">
                    <Toggle
                      label="Enable subtitles by default"
                      checked={current.subtitles_enabled_by_default}
                      onChange={(checked) =>
                        setLibraryField(library.id, "subtitles_enabled_by_default", checked)
                      }
                      description="If the preferred subtitle language exists, Plum will enable it automatically."
                    />
                  </div>
                </div>

                <p
                  className={`mt-4 text-sm ${
                    message?.includes("saved")
                      ? "text-emerald-300"
                      : message
                        ? "text-red-300"
                        : isDirty
                          ? "text-[var(--plum-muted)]"
                          : "text-[var(--plum-muted)]"
                  }`}
                >
                  {message ?? (isDirty ? "Unsaved changes." : "Defaults are active for new playback sessions.")}
                </p>
              </article>
            );
          })}
        </div>
      )}
    </section>
  );

  if (!isAdmin) {
    return (
      <div className="mx-auto flex max-w-5xl flex-col gap-6">
        {playbackDefaultsSection}
        <div className="rounded-[var(--radius-lg)] border border-[var(--plum-border)] bg-[var(--plum-panel)]/80 p-6">
          <h2 className="text-xl font-semibold text-[var(--plum-text)]">Transcoding</h2>
          <p className="mt-2 text-sm text-[var(--plum-muted)]">
            Server transcoding settings are only available to admin accounts.
          </p>
        </div>
      </div>
    );
  }

  if (settingsQuery.isError) {
    return (
      <div className="mx-auto flex max-w-5xl flex-col gap-6">
        {playbackDefaultsSection}
        <div className="rounded-[var(--radius-lg)] border border-[var(--plum-border)] bg-[var(--plum-panel)]/80 p-6">
          <h2 className="text-xl font-semibold text-[var(--plum-text)]">Transcoding</h2>
          <p className="mt-2 text-sm text-red-300">
            {settingsQuery.error.message || "Failed to load transcoding settings."}
          </p>
        </div>
      </div>
    );
  }

  if (settingsQuery.isLoading || form == null) {
    return (
      <div className="mx-auto flex max-w-5xl flex-col gap-6">
        {playbackDefaultsSection}
        <div className="rounded-[var(--radius-lg)] border border-[var(--plum-border)] bg-[var(--plum-panel)]/80 p-6">
          <h2 className="text-xl font-semibold text-[var(--plum-text)]">Transcoding</h2>
          <p className="mt-2 text-sm text-[var(--plum-muted)]">Loading transcoding settings…</p>
        </div>
      </div>
    );
  }

  function setField<K extends keyof TranscodingSettingsShape>(
    key: K,
    value: TranscodingSettingsShape[K],
  ) {
    setForm((current) => (current ? { ...current, [key]: value } : current));
    setDirty(true);
    setSaveMessage(null);
  }

  const setDecodeCodec = (key: VaapiDecodeCodec, checked: boolean) => {
    setForm((current) =>
      current
        ? {
            ...current,
            decodeCodecs: { ...current.decodeCodecs, [key]: checked },
          }
        : current,
    );
    setDirty(true);
    setSaveMessage(null);
  };

  const setEncodeFormat = (key: HardwareEncodeFormat, checked: boolean) => {
    setForm((current) => {
      if (!current) return current;
      const next = {
        ...current,
        encodeFormats: { ...current.encodeFormats, [key]: checked },
      };
      if (!next.encodeFormats[next.preferredHardwareEncodeFormat]) {
        const fallback =
          encodeFormatOptions.find((option) => next.encodeFormats[option.key])?.key ?? "h264";
        next.preferredHardwareEncodeFormat = fallback;
      }
      return next;
    });
    setDirty(true);
    setSaveMessage(null);
  };

  const handleSave = async () => {
    if (!form) return;
    setSaveMessage(null);
    try {
      const response = await updateSettings.mutateAsync(form);
      setForm(cloneSettings(response.settings));
      setWarnings(response.warnings);
      setDirty(false);
      setSaveMessage("Transcoding settings saved.");
    } catch (error) {
      setSaveMessage(
        error instanceof Error ? error.message : "Failed to save transcoding settings.",
      );
    }
  };

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6">
      {playbackDefaultsSection}

      <section className="rounded-[var(--radius-lg)] border border-[var(--plum-border)] bg-[var(--plum-panel)]/80 p-6 shadow-[0_20px_45px_rgba(0,0,0,0.35)]">
        <div className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
          <div>
            <h1 className="text-2xl font-semibold text-[var(--plum-text)]">Transcoding</h1>
            <p className="mt-1 max-w-2xl text-sm text-[var(--plum-muted)]">
              Configure server-wide VAAPI decode and hardware encode behavior for future transcode
              jobs.
            </p>
          </div>
          <Button onClick={handleSave} disabled={updateSettings.isPending}>
            {updateSettings.isPending ? "Saving…" : "Save settings"}
          </Button>
        </div>
      </section>

      <section className="grid gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(18rem,1fr)]">
        <div className="flex flex-col gap-6">
          <div className="rounded-[var(--radius-lg)] border border-[var(--plum-border)] bg-[var(--plum-panel)]/80 p-6">
            <div className="flex items-start justify-between gap-4">
              <div>
                <h2 className="text-lg font-medium text-[var(--plum-text)]">
                  Video Acceleration API
                </h2>
                <p className="mt-1 text-sm text-[var(--plum-muted)]">
                  Enable VAAPI on the server and choose which source codecs are allowed to use it
                  for decode.
                </p>
              </div>
              <Toggle
                label="Enable VAAPI"
                checked={form.vaapiEnabled}
                onChange={(checked) => setField("vaapiEnabled", checked)}
              />
            </div>

            <div className="mt-5 space-y-5">
              <div>
                <label
                  className="mb-2 block text-sm font-medium text-[var(--plum-text)]"
                  htmlFor="vaapi-device"
                >
                  VAAPI device
                </label>
                <Input
                  id="vaapi-device"
                  value={form.vaapiDevicePath}
                  onChange={(event) => setField("vaapiDevicePath", event.target.value)}
                  placeholder="/dev/dri/renderD128"
                />
                <p className="mt-2 text-xs text-[var(--plum-muted)]">
                  Default render node for Intel/AMD VAAPI on Linux hosts.
                </p>
              </div>

              <div>
                <div className="mb-3">
                  <h3 className="text-sm font-medium text-[var(--plum-text)]">Decode codecs</h3>
                  <p className="mt-1 text-xs text-[var(--plum-muted)]">
                    Each codec can be enabled or disabled independently. Disabled codecs stay on
                    software decode.
                  </p>
                </div>
                <div className="grid gap-3 md:grid-cols-2">
                  {decodeCodecOptions.map((option) => (
                    <CheckboxCard
                      key={option.key}
                      checked={form.decodeCodecs[option.key]}
                      label={option.label}
                      description={option.description}
                      onChange={(checked) => setDecodeCodec(option.key, checked)}
                    />
                  ))}
                </div>
              </div>
            </div>
          </div>

          <div className="rounded-[var(--radius-lg)] border border-[var(--plum-border)] bg-[var(--plum-panel)]/80 p-6">
            <div className="flex items-start justify-between gap-4">
              <div>
                <h2 className="text-lg font-medium text-[var(--plum-text)]">Hardware encoding</h2>
                <p className="mt-1 text-sm text-[var(--plum-muted)]">
                  Use VAAPI encoders when possible, with automatic software fallback if the hardware
                  path fails.
                </p>
              </div>
              <Toggle
                label="Enable hardware encoding"
                checked={form.hardwareEncodingEnabled}
                onChange={(checked) => setField("hardwareEncodingEnabled", checked)}
              />
            </div>

            <div className="mt-5 space-y-5">
              <div>
                <div className="mb-3">
                  <h3 className="text-sm font-medium text-[var(--plum-text)]">
                    Allowed output formats
                  </h3>
                  <p className="mt-1 text-xs text-[var(--plum-muted)]">
                    H.264 is enabled by default. HEVC and AV1 stay opt-in for compatibility and host
                    support reasons.
                  </p>
                </div>
                <div className="grid gap-3 md:grid-cols-3">
                  {encodeFormatOptions.map((option) => (
                    <CheckboxCard
                      key={option.key}
                      checked={form.encodeFormats[option.key]}
                      label={option.label}
                      description={option.description}
                      onChange={(checked) => setEncodeFormat(option.key, checked)}
                    />
                  ))}
                </div>
              </div>

              <div>
                <label
                  className="mb-2 block text-sm font-medium text-[var(--plum-text)]"
                  htmlFor="preferred-encode-format"
                >
                  Preferred hardware encode format
                </label>
                <select
                  id="preferred-encode-format"
                  value={form.preferredHardwareEncodeFormat}
                  onChange={(event) =>
                    setField(
                      "preferredHardwareEncodeFormat",
                      event.target.value as HardwareEncodeFormat,
                    )
                  }
                  className="flex h-9 w-full rounded-[var(--radius-md)] border border-[var(--plum-border)] bg-[var(--plum-panel)] px-3 py-1 text-sm text-[var(--plum-text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--plum-ring)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--plum-bg)]"
                >
                  {encodeFormatOptions.map((option) => (
                    <option
                      key={option.key}
                      value={option.key}
                      disabled={!form.encodeFormats[option.key]}
                    >
                      {option.label}
                    </option>
                  ))}
                </select>
                <p className="mt-2 text-xs text-[var(--plum-muted)]">
                  Plum will try this hardware output format first, then retry in software if enabled
                  below.
                </p>
              </div>

              <Toggle
                label="Allow automatic software fallback"
                checked={form.allowSoftwareFallback}
                onChange={(checked) => setField("allowSoftwareFallback", checked)}
                description="When hardware transcoding fails, retry with software-safe FFmpeg settings."
              />
            </div>
          </div>
        </div>

        <aside className="flex flex-col gap-4">
          <div className="rounded-[var(--radius-lg)] border border-[var(--plum-border)] bg-[var(--plum-panel)]/80 p-5">
            <h2 className="text-sm font-semibold uppercase tracking-[0.18em] text-[var(--plum-muted)]">
              Host warnings
            </h2>
            {warnings.length === 0 ? (
              <p className="mt-3 text-sm text-[var(--plum-muted)]">
                No capability warnings reported for the current server configuration.
              </p>
            ) : (
              <ul className="mt-3 space-y-3">
                {warnings.map((warning) => (
                  <li
                    key={warning.code}
                    className="rounded-[var(--radius-md)] border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-100"
                  >
                    {warning.message}
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="rounded-[var(--radius-lg)] border border-[var(--plum-border)] bg-[var(--plum-panel)]/80 p-5">
            <h2 className="text-sm font-semibold uppercase tracking-[0.18em] text-[var(--plum-muted)]">
              Save status
            </h2>
            <p
              className={`mt-3 text-sm ${
                saveMessage?.includes("saved")
                  ? "text-emerald-300"
                  : saveMessage
                    ? "text-red-300"
                    : "text-[var(--plum-muted)]"
              }`}
            >
              {saveMessage ??
                (dirty ? "Unsaved changes." : "Saved settings are active for future jobs.")}
            </p>
          </div>
        </aside>
      </section>
    </div>
  );
}

function Toggle({
  label,
  checked,
  onChange,
  description,
}: {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  description?: string;
}) {
  return (
    <label className="inline-flex cursor-pointer items-start gap-3 text-sm text-[var(--plum-text)]">
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
        className="mt-0.5 size-4 rounded border-[var(--plum-border)] bg-[var(--plum-panel-alt)] accent-[var(--plum-accent)]"
      />
      <span className="flex flex-col">
        <span>{label}</span>
        {description ? (
          <span className="text-xs text-[var(--plum-muted)]">{description}</span>
        ) : null}
      </span>
    </label>
  );
}

function CheckboxCard({
  checked,
  label,
  description,
  onChange,
}: {
  checked: boolean;
  label: string;
  description: string;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="flex cursor-pointer gap-3 rounded-[var(--radius-md)] border border-[var(--plum-border)] bg-[var(--plum-panel-alt)]/60 p-3 transition-colors hover:border-[var(--plum-accent-soft)]">
      <input
        type="checkbox"
        checked={checked}
        aria-label={label}
        onChange={(event) => onChange(event.target.checked)}
        className="mt-1 size-4 rounded border-[var(--plum-border)] bg-[var(--plum-panel-alt)] accent-[var(--plum-accent)]"
      />
      <span className="flex min-w-0 flex-col">
        <span className="text-sm font-medium text-[var(--plum-text)]">{label}</span>
        <span className="text-xs text-[var(--plum-muted)]">{description}</span>
      </span>
    </label>
  );
}
