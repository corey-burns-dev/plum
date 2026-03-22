import type {
  AttachPlaybackSessionCommand,
  CreateLibraryPayload,
  CredentialsPayload,
  CreatePlaybackSessionPayload,
  DetachPlaybackSessionCommand,
  EmbeddedAudioTrack,
  EmbeddedSubtitle,
  HardwareEncodeFormat,
  HomeDashboard,
  IdentifyResult,
  Library,
  LibraryScanStatus,
  LibraryType,
  MatchStatus,
  MediaItem,
  PlumWebSocketCommand,
  PlumWebSocketEvent,
  PlaybackSession,
  RecentlyAddedEntry,
  ScanLibraryResult,
  SeriesDetails,
  SeriesSearchResult,
  SetupStatus,
  ShowActionResult,
  ShowConfirmPayload,
  Subtitle,
  TranscodingSettings,
  TranscodingSettingsResponse,
  TranscodingSettingsWarning,
  UpdatePlaybackSessionAudioPayload,
  UpdateLibraryPlaybackPreferencesPayload,
  UpdateMediaProgressPayload,
  User,
  VaapiDecodeCodec,
} from "@plum/contracts";
import {
  AttachPlaybackSessionCommandSchema,
  CreateLibraryPayloadSchema,
  CredentialsPayloadSchema,
  CreatePlaybackSessionPayloadSchema,
  DetachPlaybackSessionCommandSchema,
  HomeDashboardSchema,
  IdentifyResultSchema,
  LibrarySchema,
  LibraryScanStatusSchema,
  MediaItemSchema,
  PlaybackSessionSchema,
  PlumWebSocketCommandSchema,
  PlumWebSocketEventSchema,
  ScanLibraryResultSchema,
  SeriesDetailsSchema,
  SeriesSearchResultSchema,
  SetupStatusSchema,
  ShowActionResultSchema,
  ShowConfirmPayloadSchema,
  TranscodingSettingsResponseSchema,
  TranscodingSettingsSchema,
  UpdatePlaybackSessionAudioPayloadSchema,
  UpdateLibraryPlaybackPreferencesPayloadSchema,
  UpdateMediaProgressPayloadSchema,
  UserSchema,
} from "@plum/contracts";
import { Data, Effect, Option, Schema, Schedule } from "effect";
import { buildBackendUrl, ensureBaseUrl } from "./backend";

export type {
  AttachPlaybackSessionCommand,
  CreateLibraryPayload,
  CredentialsPayload,
  CreatePlaybackSessionPayload,
  DetachPlaybackSessionCommand,
  EmbeddedAudioTrack,
  EmbeddedSubtitle,
  HardwareEncodeFormat,
  HomeDashboard,
  IdentifyResult,
  Library,
  LibraryScanStatus,
  LibraryType,
  MatchStatus,
  MediaItem,
  PlumWebSocketCommand,
  PlumWebSocketEvent,
  PlaybackSession,
  RecentlyAddedEntry,
  ScanLibraryResult,
  SeriesDetails,
  SeriesSearchResult,
  SetupStatus,
  ShowActionResult,
  ShowConfirmPayload,
  Subtitle,
  TranscodingSettings,
  TranscodingSettingsResponse,
  TranscodingSettingsWarning,
  UpdatePlaybackSessionAudioPayload,
  UpdateLibraryPlaybackPreferencesPayload,
  UpdateMediaProgressPayload,
  User,
  VaapiDecodeCodec,
};

const defaultFetchOptions = { credentials: "include" } satisfies RequestInit;

export class ApiNetworkError extends Data.TaggedError("ApiNetworkError")<{
  readonly method: string;
  readonly url: string;
  readonly message: string;
  readonly cause: unknown;
}> {}

export class ApiAbortedError extends Data.TaggedError("ApiAbortedError")<{
  readonly method: string;
  readonly url: string;
  readonly message: string;
}> {}

export class ApiHttpError extends Data.TaggedError("ApiHttpError")<{
  readonly method: string;
  readonly url: string;
  readonly status: number;
  readonly body: string;
  readonly message: string;
}> {}

export class ApiJsonError extends Data.TaggedError("ApiJsonError")<{
  readonly method: string;
  readonly url: string;
  readonly message: string;
  readonly cause: unknown;
}> {}

export class ApiDecodeError extends Data.TaggedError("ApiDecodeError")<{
  readonly method: string;
  readonly url: string;
  readonly message: string;
  readonly cause: unknown;
}> {}

export class IdentifyTimeoutError extends Data.TaggedError("IdentifyTimeoutError")<{
  readonly libraryId: number;
  readonly message: string;
}> {}

export type PlumApiError =
  | ApiAbortedError
  | ApiDecodeError
  | ApiHttpError
  | ApiJsonError
  | ApiNetworkError;

export type IdentifyLibraryTaskError = PlumApiError | IdentifyTimeoutError;

export interface AuthSessionSnapshot {
  hasAdmin: boolean;
  user: User | null;
}

export interface CreatePlumApiClientOptions {
  readonly baseUrl: string;
  readonly fetch?: typeof fetch;
  readonly fetchOptions?: RequestInit;
}

interface JsonRequestOptions<S extends Schema.Top & { readonly DecodingServices: never }> {
  readonly path: string;
  readonly schema: S;
  readonly method?: string;
  readonly body?: unknown;
  readonly signal?: AbortSignal;
  readonly handleResponse?: (
    response: Response,
    url: string,
  ) => Effect.Effect<S["Type"], PlumApiError> | null;
  readonly errorMessage?: (details: { readonly status: number; readonly body: string }) => string;
}

interface VoidRequestOptions {
  readonly path: string;
  readonly method?: string;
  readonly body?: unknown;
  readonly signal?: AbortSignal;
  readonly handleResponse?: (
    response: Response,
    url: string,
  ) => Effect.Effect<void, PlumApiError> | null;
  readonly errorMessage?: (details: { readonly status: number; readonly body: string }) => string;
}

type RequestSignal = {
  readonly signal: AbortSignal;
  readonly cleanup: () => void;
};

function isAbortError(cause: unknown): boolean {
  return cause instanceof DOMException
    ? cause.name === "AbortError"
    : cause instanceof Error && cause.name === "AbortError";
}

function combineAbortSignals(primary: AbortSignal, secondary?: AbortSignal): RequestSignal {
  if (!secondary) {
    return { signal: primary, cleanup: () => {} };
  }

  const abortSignalAny = AbortSignal as typeof AbortSignal & {
    any?: (signals: Iterable<AbortSignal>) => AbortSignal;
  };

  if (typeof abortSignalAny.any === "function") {
    return {
      signal: abortSignalAny.any([primary, secondary]),
      cleanup: () => {},
    };
  }

  const controller = new AbortController();
  const abort = () => controller.abort();

  if (primary.aborted || secondary.aborted) {
    controller.abort();
    return { signal: controller.signal, cleanup: () => {} };
  }

  primary.addEventListener("abort", abort, { once: true });
  secondary.addEventListener("abort", abort, { once: true });

  return {
    signal: controller.signal,
    cleanup: () => {
      primary.removeEventListener("abort", abort);
      secondary.removeEventListener("abort", abort);
    },
  };
}

function buildScanQuery(options?: { readonly identify?: boolean; readonly subpath?: string }): string {
  const params = new URLSearchParams();
  if (options?.identify === false) {
    params.set("identify", "false");
  }
  if (options?.subpath && options.subpath.trim() !== "") {
    params.set("subpath", options.subpath);
  }
  const query = params.toString();
  return query === "" ? "" : `?${query}`;
}

export function effectErrorToError(error: PlumApiError | IdentifyTimeoutError): Error {
  return new Error(error.message);
}

function decodeSchemaEffect<S extends Schema.Top & { readonly DecodingServices: never }>(
  schema: S,
  payload: unknown,
  method: string,
  url: string,
  message: string,
): Effect.Effect<S["Type"], ApiDecodeError> {
  return Schema.decodeUnknownEffect(schema)(payload).pipe(
    Effect.mapError(
      (cause) =>
        new ApiDecodeError({
          method,
          url,
          message,
          cause,
        }),
    ),
  );
}

export function parsePlumWebSocketEvent(raw: string): PlumWebSocketEvent | null {
  try {
    const parsed = JSON.parse(raw);
    return Schema.decodeUnknownSync(PlumWebSocketEventSchema)(parsed);
  } catch {
    return null;
  }
}

export function parsePlumWebSocketCommand(raw: string): PlumWebSocketCommand | null {
  try {
    const parsed = JSON.parse(raw);
    return Schema.decodeUnknownSync(PlumWebSocketCommandSchema)(parsed);
  } catch {
    return null;
  }
}

export function serializePlumWebSocketCommand(command: PlumWebSocketCommand): string {
  const schema =
    command.action === "attach_playback_session"
      ? AttachPlaybackSessionCommandSchema
      : DetachPlaybackSessionCommandSchema;
  return JSON.stringify(Schema.decodeUnknownSync(schema)(command));
}

export function buildPlumWebSocketUrl(baseUrl: string, origin: string): string {
  const normalizedBase = ensureBaseUrl(baseUrl);
  const resolvedBase = normalizedBase
    ? normalizedBase.startsWith("http")
      ? new URL(normalizedBase)
      : new URL(normalizedBase, origin)
    : new URL(origin);
  const protocol = resolvedBase.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${resolvedBase.host}${resolvedBase.pathname.replace(/\/$/, "")}/ws`;
}

export function createPlumApiClient(options: CreatePlumApiClientOptions) {
  const baseUrl = ensureBaseUrl(options.baseUrl);
  const fetchImpl = options.fetch ?? globalThis.fetch.bind(globalThis);
  const mergedFetchOptions = {
    ...defaultFetchOptions,
    ...options.fetchOptions,
  };

  const requestEffect = ({
    method = "GET",
    path,
    body,
    signal,
  }: {
    readonly method?: string;
    readonly path: string;
    readonly body?: unknown;
    readonly signal?: AbortSignal;
  }): Effect.Effect<Response, ApiAbortedError | ApiNetworkError> => {
    const url = buildBackendUrl(baseUrl, path);
    return Effect.tryPromise({
      try: (effectSignal) => {
        const { signal: requestSignal, cleanup } = combineAbortSignals(effectSignal, signal);
        return fetchImpl(url, {
          ...mergedFetchOptions,
          method,
          headers: body === undefined ? undefined : { "Content-Type": "application/json" },
          body: body === undefined ? undefined : JSON.stringify(body),
          signal: requestSignal,
        }).finally(cleanup);
      },
      catch: (cause) =>
        isAbortError(cause)
          ? new ApiAbortedError({
              method,
              url,
              message: `${method} ${url} was aborted.`,
            })
          : new ApiNetworkError({
              method,
              url,
              message: `${method} ${url} failed.`,
              cause,
            }),
    });
  };

  const readTextEffect = (
    response: Response,
    method: string,
    url: string,
  ): Effect.Effect<string, ApiAbortedError | ApiJsonError> =>
    Effect.tryPromise({
      try: () => response.text(),
      catch: (cause) =>
        isAbortError(cause)
          ? new ApiAbortedError({
              method,
              url,
              message: `${method} ${url} was aborted.`,
            })
          : new ApiJsonError({
              method,
              url,
              message: `Failed to read response body from ${method} ${url}.`,
              cause,
            }),
    });

  const readJsonEffect = (
    response: Response,
    method: string,
    url: string,
  ): Effect.Effect<unknown, ApiAbortedError | ApiJsonError> =>
    Effect.tryPromise({
      try: () => response.json(),
      catch: (cause) =>
        isAbortError(cause)
          ? new ApiAbortedError({
              method,
              url,
              message: `${method} ${url} was aborted.`,
            })
          : new ApiJsonError({
              method,
              url,
              message: `Invalid JSON response from ${method} ${url}.`,
              cause,
            }),
    });

  const failHttpEffect = (
    response: Response,
    method: string,
    url: string,
    errorMessage?: (details: { readonly status: number; readonly body: string }) => string,
  ): Effect.Effect<never, ApiAbortedError | ApiHttpError | ApiJsonError> =>
    readTextEffect(response, method, url).pipe(
      Effect.catchTag("ApiJsonError", () => Effect.succeed("")),
      Effect.flatMap((body) =>
        Effect.fail(
          new ApiHttpError({
            method,
            url,
            status: response.status,
            body,
            message:
              errorMessage?.({ status: response.status, body }) ??
              `${method} ${url} failed with status ${response.status}.`,
          }),
        ),
      ),
    );

  const jsonRequestEffect = <S extends Schema.Top & { readonly DecodingServices: never }>(
    request: JsonRequestOptions<S>,
  ): Effect.Effect<S["Type"], PlumApiError> => {
    const method = request.method ?? "GET";
    const url = buildBackendUrl(baseUrl, request.path);
    return requestEffect({
      method,
      path: request.path,
      body: request.body,
      signal: request.signal,
    }).pipe(
      Effect.flatMap((response) => {
        const handled: Effect.Effect<S["Type"], PlumApiError> | null | undefined =
          request.handleResponse?.(response, url);
        if (handled) {
          return handled;
        }
        if (!response.ok) {
          return failHttpEffect(response, method, url, request.errorMessage);
        }
        return readJsonEffect(response, method, url).pipe(
          Effect.flatMap((payload) =>
            decodeSchemaEffect(
              request.schema,
              payload,
              method,
              url,
              `Invalid response payload from ${method} ${url}.`,
            ),
          ),
        );
      }),
    );
  };

  const voidRequestEffect = (request: VoidRequestOptions): Effect.Effect<void, PlumApiError> => {
    const method = request.method ?? "GET";
    const url = buildBackendUrl(baseUrl, request.path);
    return requestEffect({
      method,
      path: request.path,
      body: request.body,
      signal: request.signal,
    }).pipe(
      Effect.flatMap((response) => {
        const handled = request.handleResponse?.(response, url);
        if (handled) {
          return handled;
        }
        if (!response.ok) {
          return failHttpEffect(response, method, url, request.errorMessage);
        }
        return Effect.void;
      }),
    );
  };

  const run = <A>(effect: Effect.Effect<A, PlumApiError>): Promise<A> =>
    Effect.runPromise(effect.pipe(Effect.mapError(effectErrorToError)));

  const effects = {
    getSetupStatus: () =>
      jsonRequestEffect({
        path: "/api/setup/status",
        schema: SetupStatusSchema,
        errorMessage: ({ status }) => `Setup status: ${status}`,
      }),
    createAdmin: (payload: CredentialsPayload) =>
      decodeSchemaEffect(
        CredentialsPayloadSchema,
        payload,
        "POST",
        "/api/auth/admin-setup",
        "Invalid admin setup payload.",
      ).pipe(
        Effect.flatMap((validatedPayload) =>
          jsonRequestEffect({
            method: "POST",
            path: "/api/auth/admin-setup",
            schema: UserSchema,
            body: validatedPayload,
            errorMessage: ({ status, body }) =>
              status === 409 ? "Admin already exists." : body || `Failed: ${status}`,
          }),
        ),
      ),
    login: (payload: CredentialsPayload) =>
      decodeSchemaEffect(
        CredentialsPayloadSchema,
        payload,
        "POST",
        "/api/auth/login",
        "Invalid login payload.",
      ).pipe(
        Effect.flatMap((validatedPayload) =>
          jsonRequestEffect({
            method: "POST",
            path: "/api/auth/login",
            schema: UserSchema,
            body: validatedPayload,
            errorMessage: ({ body }) => body || "Invalid credentials.",
          }),
        ),
      ),
    logout: () =>
      voidRequestEffect({
        method: "POST",
        path: "/api/auth/logout",
        handleResponse: () => Effect.void,
      }),
    getMe: () =>
      jsonRequestEffect({
        path: "/api/auth/me",
        schema: Schema.NullOr(UserSchema),
        handleResponse: (response, url) =>
          response.status === 401
            ? Effect.succeed<User | null>(null)
            : !response.ok
              ? failHttpEffect(response, "GET", url, ({ status }) => `Me: ${status}`)
              : null,
      }),
    createLibrary: (payload: CreateLibraryPayload) =>
      decodeSchemaEffect(
        CreateLibraryPayloadSchema,
        payload,
        "POST",
        "/api/libraries",
        "Invalid create library payload.",
      ).pipe(
        Effect.flatMap((validatedPayload) =>
          jsonRequestEffect({
            method: "POST",
            path: "/api/libraries",
            schema: LibrarySchema,
            body: validatedPayload,
            errorMessage: ({ status, body }) => body || `Create library: ${status}`,
          }),
        ),
      ),
    listLibraries: () =>
      jsonRequestEffect({
        path: "/api/libraries",
        schema: Schema.Array(LibrarySchema),
        errorMessage: ({ status, body }) => `Libraries: ${status}${body ? ` ${body}` : ""}`,
      }),
    updateLibraryPlaybackPreferences: (
      id: number,
      payload: UpdateLibraryPlaybackPreferencesPayload,
    ) =>
      decodeSchemaEffect(
        UpdateLibraryPlaybackPreferencesPayloadSchema,
        payload,
        "PUT",
        `/api/libraries/${id}/playback-preferences`,
        "Invalid library playback preferences payload.",
      ).pipe(
        Effect.flatMap((validatedPayload) =>
          jsonRequestEffect({
            method: "PUT",
            path: `/api/libraries/${id}/playback-preferences`,
            schema: LibrarySchema,
            body: validatedPayload,
            errorMessage: ({ status, body }) =>
              body || `Save library playback preferences: ${status}`,
          }),
        ),
      ),
    getLibraryScanStatus: (id: number) =>
      jsonRequestEffect({
        path: `/api/libraries/${id}/scan`,
        schema: LibraryScanStatusSchema,
        errorMessage: ({ status, body }) => body || `Scan status: ${status}`,
      }),
    startLibraryScan: (
      id: number,
      options?: { readonly identify?: boolean; readonly subpath?: string },
    ) =>
      jsonRequestEffect({
        method: "POST",
        path: `/api/libraries/${id}/scan/start${buildScanQuery(options)}`,
        schema: LibraryScanStatusSchema,
        errorMessage: ({ status, body }) => body || `Start scan: ${status}`,
      }),
    scanLibraryById: (
      id: number,
      options?: { readonly identify?: boolean; readonly subpath?: string },
    ) =>
      jsonRequestEffect({
        method: "POST",
        path: `/api/libraries/${id}/scan${buildScanQuery(options)}`,
        schema: ScanLibraryResultSchema,
        errorMessage: ({ status, body }) => body || `Scan: ${status}`,
      }),
    identifyLibrary: (id: number, options?: { readonly signal?: AbortSignal }) =>
      jsonRequestEffect({
        method: "POST",
        path: `/api/libraries/${id}/identify`,
        schema: IdentifyResultSchema,
        signal: options?.signal,
        errorMessage: ({ status, body }) => body || `Identify: ${status}`,
      }),
    fetchSeriesByTmdbId: (tmdbId: number) =>
      jsonRequestEffect({
        path: `/api/series/${tmdbId}`,
        schema: Schema.NullOr(SeriesDetailsSchema),
        handleResponse: (response, url) =>
          response.status === 404
            ? Effect.succeed<SeriesDetails | null>(null)
            : !response.ok
              ? failHttpEffect(response, "GET", url, ({ status }) => `Series: ${status}`)
              : null,
      }),
    fetchLibraryMedia: (id: number) =>
      jsonRequestEffect({
        path: `/api/libraries/${id}/media`,
        schema: Schema.Array(MediaItemSchema),
        errorMessage: ({ status, body }) => `Library media: ${status}${body ? ` ${body}` : ""}`,
      }),
    getHomeDashboard: () =>
      jsonRequestEffect({
        path: "/api/home",
        schema: HomeDashboardSchema,
        errorMessage: ({ status, body }) => `Home: ${status}${body ? ` ${body}` : ""}`,
      }),
    fetchMediaList: () =>
      jsonRequestEffect({
        path: "/api/media",
        schema: Schema.Array(MediaItemSchema),
        errorMessage: ({ status }) => `Failed to fetch media: ${status}`,
      }),
    updateMediaProgress: (id: number, payload: UpdateMediaProgressPayload) =>
      decodeSchemaEffect(
        UpdateMediaProgressPayloadSchema,
        payload,
        "PUT",
        `/api/media/${id}/progress`,
        "Invalid media progress payload.",
      ).pipe(
        Effect.flatMap((validatedPayload) =>
          voidRequestEffect({
            method: "PUT",
            path: `/api/media/${id}/progress`,
            body: validatedPayload,
            errorMessage: ({ status, body }) => body || `Progress: ${status}`,
          }),
        ),
      ),
    createPlaybackSession: (id: number, payload?: CreatePlaybackSessionPayload) =>
      (payload
        ? decodeSchemaEffect(
            CreatePlaybackSessionPayloadSchema,
            payload,
            "POST",
            `/api/playback/sessions/${id}`,
            "Invalid playback session payload.",
          )
        : Effect.succeed<CreatePlaybackSessionPayload>({})
      ).pipe(
        Effect.flatMap((validatedPayload) =>
          jsonRequestEffect({
            method: "POST",
            path: `/api/playback/sessions/${id}`,
            schema: PlaybackSessionSchema,
            body: validatedPayload,
            errorMessage: ({ status, body }) => body || `Create playback session: ${status}`,
          }),
        ),
      ),
    updatePlaybackSessionAudio: (sessionId: string, payload: UpdatePlaybackSessionAudioPayload) =>
      decodeSchemaEffect(
        UpdatePlaybackSessionAudioPayloadSchema,
        payload,
        "PATCH",
        `/api/playback/sessions/${sessionId}/audio`,
        "Invalid playback session audio payload.",
      ).pipe(
        Effect.flatMap((validatedPayload) =>
          jsonRequestEffect({
            method: "PATCH",
            path: `/api/playback/sessions/${sessionId}/audio`,
            schema: PlaybackSessionSchema,
            body: validatedPayload,
            errorMessage: ({ status, body }) => body || `Update playback session audio: ${status}`,
          }),
        ),
      ),
    closePlaybackSession: (sessionId: string) =>
      voidRequestEffect({
        method: "DELETE",
        path: `/api/playback/sessions/${sessionId}`,
        errorMessage: ({ status, body }) => body || `Close playback session: ${status}`,
      }),
    getTranscodingSettings: () =>
      jsonRequestEffect({
        path: "/api/settings/transcoding",
        schema: TranscodingSettingsResponseSchema,
        errorMessage: ({ status, body }) => body || `Transcoding settings: ${status}`,
      }),
    updateTranscodingSettings: (payload: TranscodingSettings) =>
      decodeSchemaEffect(
        TranscodingSettingsSchema,
        payload,
        "PUT",
        "/api/settings/transcoding",
        "Invalid transcoding settings payload.",
      ).pipe(
        Effect.flatMap((validatedPayload) =>
          jsonRequestEffect({
            method: "PUT",
            path: "/api/settings/transcoding",
            schema: TranscodingSettingsResponseSchema,
            body: validatedPayload,
            errorMessage: ({ status, body }) => body || `Save transcoding settings: ${status}`,
          }),
        ),
      ),
    searchSeries: (query: string) => {
      const trimmed = query.trim();
      if (!trimmed) {
        return Effect.succeed<SeriesSearchResult[]>([]);
      }
      return jsonRequestEffect({
        path: `/api/series/search?q=${encodeURIComponent(trimmed)}`,
        schema: Schema.Array(SeriesSearchResultSchema),
        errorMessage: ({ status }) => `Search: ${status}`,
      });
    },
    refreshShow: (libraryId: number, showKey: string) =>
      jsonRequestEffect({
        method: "POST",
        path: `/api/libraries/${libraryId}/shows/refresh`,
        schema: ShowActionResultSchema,
        body: { showKey },
        errorMessage: ({ status }) => `Refresh: ${status}`,
      }),
    confirmShow: (libraryId: number, payload: ShowConfirmPayload) =>
      decodeSchemaEffect(
        ShowConfirmPayloadSchema,
        payload,
        "POST",
        `/api/libraries/${libraryId}/shows/confirm`,
        "Invalid show confirm payload.",
      ).pipe(
        Effect.flatMap((validatedPayload) =>
          jsonRequestEffect({
            method: "POST",
            path: `/api/libraries/${libraryId}/shows/confirm`,
            schema: ShowActionResultSchema,
            body: validatedPayload,
            errorMessage: ({ status }) => `Confirm show: ${status}`,
          }),
        ),
      ),
    identifyShow: (libraryId: number, showKey: string, tmdbId: number) =>
      jsonRequestEffect({
        method: "POST",
        path: `/api/libraries/${libraryId}/shows/identify`,
        schema: ShowActionResultSchema,
        body: { showKey, tmdbId },
        errorMessage: ({ status }) => `Identify: ${status}`,
      }),
  };

  return {
    baseUrl,
    effects,
    getSetupStatus: () => run(effects.getSetupStatus()),
    createAdmin: (payload: CredentialsPayload) => run(effects.createAdmin(payload)),
    login: (payload: CredentialsPayload) => run(effects.login(payload)),
    logout: () => run(effects.logout()),
    getMe: () => run(effects.getMe()),
    createLibrary: (payload: CreateLibraryPayload) => run(effects.createLibrary(payload)),
    listLibraries: () => run(effects.listLibraries()),
    updateLibraryPlaybackPreferences: (
      id: number,
      payload: UpdateLibraryPlaybackPreferencesPayload,
    ) => run(effects.updateLibraryPlaybackPreferences(id, payload)),
    getLibraryScanStatus: (id: number) => run(effects.getLibraryScanStatus(id)),
    startLibraryScan: (
      id: number,
      options?: { readonly identify?: boolean; readonly subpath?: string },
    ) =>
      run(effects.startLibraryScan(id, options)),
    scanLibraryById: (
      id: number,
      options?: { readonly identify?: boolean; readonly subpath?: string },
    ) =>
      run(effects.scanLibraryById(id, options)),
    identifyLibrary: (id: number, options?: { readonly signal?: AbortSignal }) =>
      run(effects.identifyLibrary(id, options)),
    fetchSeriesByTmdbId: (tmdbId: number) => run(effects.fetchSeriesByTmdbId(tmdbId)),
    fetchLibraryMedia: (id: number) => run(effects.fetchLibraryMedia(id)),
    getHomeDashboard: () => run(effects.getHomeDashboard()),
    fetchMediaList: () => run(effects.fetchMediaList()),
    updateMediaProgress: (id: number, payload: UpdateMediaProgressPayload) =>
      run(effects.updateMediaProgress(id, payload)),
    createPlaybackSession: (id: number, payload?: CreatePlaybackSessionPayload) =>
      run(effects.createPlaybackSession(id, payload)),
    updatePlaybackSessionAudio: (sessionId: string, payload: UpdatePlaybackSessionAudioPayload) =>
      run(effects.updatePlaybackSessionAudio(sessionId, payload)),
    closePlaybackSession: (sessionId: string) => run(effects.closePlaybackSession(sessionId)),
    getTranscodingSettings: () => run(effects.getTranscodingSettings()),
    updateTranscodingSettings: (payload: TranscodingSettings) =>
      run(effects.updateTranscodingSettings(payload)),
    searchSeries: (query: string) => run(effects.searchSeries(query)),
    refreshShow: (libraryId: number, showKey: string) =>
      run(effects.refreshShow(libraryId, showKey)),
    confirmShow: (libraryId: number, payload: ShowConfirmPayload) =>
      run(effects.confirmShow(libraryId, payload)),
    identifyShow: (libraryId: number, showKey: string, tmdbId: number) =>
      run(effects.identifyShow(libraryId, showKey, tmdbId)),
  };
}

export type PlumApiClient = ReturnType<typeof createPlumApiClient>;

export function loadAuthSessionEffect(
  api: PlumApiClient,
  options?: {
    readonly retries?: number;
    readonly retryDelayMs?: number;
  },
): Effect.Effect<AuthSessionSnapshot, PlumApiError> {
  const retries = options?.retries ?? 5;
  const retryDelayMs = options?.retryDelayMs ?? 1_000;

  return Effect.gen(function* () {
    const status = yield* api.effects.getSetupStatus();
    const user = status.hasAdmin ? yield* api.effects.getMe() : null;
    return {
      hasAdmin: status.hasAdmin,
      user,
    };
  }).pipe(
    Effect.retry({
      schedule: Schedule.addDelay(Schedule.recurs(retries), () => Effect.succeed(retryDelayMs)),
    }),
  );
}

export function identifyLibraryTaskEffect(
  api: PlumApiClient,
  options: {
    readonly libraryId: number;
    readonly signal?: AbortSignal;
    readonly timeoutMs: number;
  },
): Effect.Effect<IdentifyResult, IdentifyLibraryTaskError> {
  return api.effects
    .identifyLibrary(options.libraryId, {
      signal: options.signal,
    })
    .pipe(
      Effect.timeoutOption(options.timeoutMs),
      Effect.flatMap(
        Option.match({
          onNone: () =>
            Effect.fail(
              new IdentifyTimeoutError({
                libraryId: options.libraryId,
                message: "identify-timeout",
              }),
            ),
          onSome: Effect.succeed,
        }),
      ),
    );
}

export function runIdentifyLibraryTask(
  api: PlumApiClient,
  options: {
    readonly libraryId: number;
    readonly signal?: AbortSignal;
    readonly timeoutMs: number;
  },
): Promise<IdentifyResult> {
  return Effect.runPromise(
    identifyLibraryTaskEffect(api, options).pipe(Effect.mapError(effectErrorToError)),
  );
}
