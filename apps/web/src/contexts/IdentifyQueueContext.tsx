import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useLocation } from "react-router-dom";
import { plumApiClient, type IdentifyResult } from "../api";
import { queryKeys, useLibraries } from "../queries";
import { useScanQueue } from "./ScanQueueContext";
import { runIdentifyLibraryTask } from "@plum/shared";

export type IdentifyLibraryPhase =
  | "queued"
  | "identifying"
  | "soft-reveal"
  | "identify-failed"
  | "complete";

type QueueIdentifyOptions = {
  abortActive?: boolean;
  prioritize?: boolean;
  resetState?: boolean;
};

type IdentifyQueueContextValue = {
  identifyPhases: Record<number, IdentifyLibraryPhase>;
  getLibraryPhase: (libraryId: number | null) => IdentifyLibraryPhase | undefined;
  queueLibraryIdentify: (libraryId: number, options?: QueueIdentifyOptions) => void;
};

const IDENTIFY_SOFT_REVEAL_MS = 90_000;
const IDENTIFY_HARD_TIMEOUT_MS = 180_000;
const IDENTIFY_CONCURRENT_LIBRARIES = 3;

const IdentifyQueueContext = createContext<IdentifyQueueContextValue | null>(null);

function isBackendIdentifyActive(phase?: string) {
  return phase === "queued" || phase === "identifying";
}

function isLocalIdentifyActive(phase?: IdentifyLibraryPhase) {
  return phase === "queued" || phase === "identifying";
}

function getRouteLibraryId(pathname: string): number | null {
  const match = pathname.match(/\/library\/(\d+)/);
  if (!match) return null;
  const id = parseInt(match[1], 10);
  return Number.isFinite(id) ? id : null;
}

export function IdentifyQueueProvider({ children }: { children: ReactNode }) {
  const location = useLocation();
  const queryClient = useQueryClient();
  const { data: libraries = [] } = useLibraries();
  const { getLibraryScanStatus, hasLibraryScanStatus } = useScanQueue();
  const queuedLibsRef = useRef<Set<number>>(new Set());
  const activeLibsRef = useRef<Set<number>>(new Set());
  const identifyControllersRef = useRef<Map<number, AbortController>>(new Map());
  const identifyPhasesRef = useRef<Map<number, IdentifyLibraryPhase>>(new Map());
  const identifyRetryCountsRef = useRef<Map<number, number>>(new Map());
  const identifyOrderRef = useRef<number[]>([]);
  const identifyPumpRunningRef = useRef(false);
  const [identifyPhases, setIdentifyPhases] = useState<Record<number, IdentifyLibraryPhase>>({});
  const routeLibraryId = useMemo(() => getRouteLibraryId(location.pathname), [location.pathname]);

  const setLibraryIdentifyPhase = useCallback(
    (libraryId: number, phase: IdentifyLibraryPhase | null) => {
      if (phase == null) {
        identifyPhasesRef.current.delete(libraryId);
      } else {
        identifyPhasesRef.current.set(libraryId, phase);
      }
      setIdentifyPhases((current) => {
        if (phase == null) {
          if (!(libraryId in current)) return current;
          const next = { ...current };
          delete next[libraryId];
          return next;
        }
        if (current[libraryId] === phase) return current;
        return { ...current, [libraryId]: phase };
      });
    },
    [],
  );

  const identifyLibraryWithTimers = useCallback(
    (libraryId: number) => {
      const controller = new AbortController();
      identifyControllersRef.current.set(libraryId, controller);
      return new Promise<IdentifyResult>((resolve, reject) => {
        let timedOut = false;
        const softRevealId = window.setTimeout(() => {
          if (identifyPhasesRef.current.get(libraryId) === "identifying") {
            setLibraryIdentifyPhase(libraryId, "soft-reveal");
          }
        }, IDENTIFY_SOFT_REVEAL_MS);
        const hardTimeoutId = window.setTimeout(() => {
          timedOut = true;
          controller.abort();
          reject(new Error("identify-timeout"));
        }, IDENTIFY_HARD_TIMEOUT_MS);

        runIdentifyLibraryTask(plumApiClient, {
          libraryId,
          signal: controller.signal,
          timeoutMs: IDENTIFY_HARD_TIMEOUT_MS,
        })
          .then((result) => {
            void queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) });
            void queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
            void queryClient.invalidateQueries({ queryKey: queryKeys.home });
            resolve(result);
          })
          .catch((error) => {
            if (controller.signal.aborted) {
              reject(new Error(timedOut ? "identify-timeout" : "identify-aborted"));
              return;
            }
            reject(error);
          })
          .finally(() => {
            if (identifyControllersRef.current.get(libraryId) === controller) {
              identifyControllersRef.current.delete(libraryId);
            }
            window.clearTimeout(softRevealId);
            window.clearTimeout(hardTimeoutId);
          });
      });
    },
    [queryClient, setLibraryIdentifyPhase],
  );

  const pumpIdentifyQueue = useCallback(() => {
    if (identifyPumpRunningRef.current) return;
    identifyPumpRunningRef.current = true;
    try {
      while (activeLibsRef.current.size < IDENTIFY_CONCURRENT_LIBRARIES) {
        const nextLibraryId = identifyOrderRef.current.find(
          (libraryId) =>
            queuedLibsRef.current.has(libraryId) && !activeLibsRef.current.has(libraryId),
        );
        if (nextLibraryId == null) return;

        queuedLibsRef.current.delete(nextLibraryId);
        activeLibsRef.current.add(nextLibraryId);
        setLibraryIdentifyPhase(nextLibraryId, "identifying");

        void identifyLibraryWithTimers(nextLibraryId)
          .then((result) => {
            identifyRetryCountsRef.current.delete(nextLibraryId);
            setLibraryIdentifyPhase(nextLibraryId, result.failed > 0 ? "identify-failed" : "complete");
          })
          .catch((error) => {
            if (error instanceof Error && error.message === "identify-aborted") {
              if (!queuedLibsRef.current.has(nextLibraryId)) {
                setLibraryIdentifyPhase(nextLibraryId, null);
              }
              return;
            }
            if (error instanceof Error && error.message === "identify-timeout") {
              identifyRetryCountsRef.current.delete(nextLibraryId);
              setLibraryIdentifyPhase(nextLibraryId, "soft-reveal");
              return;
            }
            const retries = identifyRetryCountsRef.current.get(nextLibraryId) ?? 0;
            if (retries < 1) {
              identifyRetryCountsRef.current.set(nextLibraryId, retries + 1);
              queuedLibsRef.current.add(nextLibraryId);
              setLibraryIdentifyPhase(nextLibraryId, "queued");
              return;
            }
            identifyRetryCountsRef.current.delete(nextLibraryId);
            setLibraryIdentifyPhase(nextLibraryId, "soft-reveal");
          })
          .finally(() => {
            activeLibsRef.current.delete(nextLibraryId);
            void Promise.resolve().then(() => pumpIdentifyQueue());
          });
      }
    } finally {
      identifyPumpRunningRef.current = false;
    }
  }, [identifyLibraryWithTimers, setLibraryIdentifyPhase]);

  const queueLibraryIdentify = useCallback(
    (libraryId: number, options?: QueueIdentifyOptions) => {
      if (options?.abortActive) {
        identifyControllersRef.current.get(libraryId)?.abort();
      }
      if (options?.resetState) setLibraryIdentifyPhase(libraryId, null);
      identifyRetryCountsRef.current.delete(libraryId);
      queuedLibsRef.current.add(libraryId);
      setLibraryIdentifyPhase(libraryId, "queued");
      if (options?.prioritize) {
        identifyOrderRef.current = [
          libraryId,
          ...identifyOrderRef.current.filter((queuedLibraryId) => queuedLibraryId !== libraryId),
        ];
      }
      void pumpIdentifyQueue();
    },
    [pumpIdentifyQueue, setLibraryIdentifyPhase],
  );

  const getLibraryPhase = useCallback(
    (libraryId: number | null) => (libraryId == null ? undefined : identifyPhases[libraryId]),
    [identifyPhases],
  );

  useEffect(() => {
    const identifyableLibraries = libraries
      .filter((library) => library.type !== "music")
      .map((library) => library.id);
    const activeIds = new Set(identifyableLibraries);
    identifyOrderRef.current =
      routeLibraryId != null && activeIds.has(routeLibraryId)
        ? [
            routeLibraryId,
            ...identifyableLibraries.filter((libraryId) => libraryId !== routeLibraryId),
          ]
        : identifyableLibraries;

    for (const libraryId of [...queuedLibsRef.current]) {
      if (!activeIds.has(libraryId)) queuedLibsRef.current.delete(libraryId);
    }
    for (const libraryId of [...activeLibsRef.current]) {
      if (!activeIds.has(libraryId)) {
        identifyControllersRef.current.get(libraryId)?.abort();
        activeLibsRef.current.delete(libraryId);
      }
    }
    for (const libraryId of [...identifyPhasesRef.current.keys()]) {
      if (!activeIds.has(libraryId)) setLibraryIdentifyPhase(libraryId, null);
    }
    for (const libraryId of [...identifyRetryCountsRef.current.keys()]) {
      if (!activeIds.has(libraryId)) identifyRetryCountsRef.current.delete(libraryId);
    }
    for (const libraryId of [...identifyControllersRef.current.keys()]) {
      if (!activeIds.has(libraryId)) identifyControllersRef.current.delete(libraryId);
    }

    for (const libraryId of identifyOrderRef.current) {
      const scanStatus = getLibraryScanStatus(libraryId);
      if (!hasLibraryScanStatus(libraryId)) {
        if (!queuedLibsRef.current.has(libraryId) && !activeLibsRef.current.has(libraryId)) {
          const identifyPhase = identifyPhasesRef.current.get(libraryId);
          if (identifyPhase === "complete" || identifyPhase === "identify-failed" || identifyPhase === "soft-reveal") {
            setLibraryIdentifyPhase(libraryId, null);
          }
        }
        continue;
      }

      const identifyPhase = identifyPhasesRef.current.get(libraryId);
      if (
        !queuedLibsRef.current.has(libraryId) &&
        !activeLibsRef.current.has(libraryId) &&
        !isBackendIdentifyActive(scanStatus?.identifyPhase) &&
        !isLocalIdentifyActive(identifyPhase)
      ) {
        if (identifyPhase === "complete" || identifyPhase === "identify-failed" || identifyPhase === "soft-reveal") {
          setLibraryIdentifyPhase(libraryId, null);
        }
      }
    }

    void pumpIdentifyQueue();
  }, [
    getLibraryScanStatus,
    hasLibraryScanStatus,
    libraries,
    pumpIdentifyQueue,
    routeLibraryId,
    setLibraryIdentifyPhase,
  ]);

  const value = useMemo<IdentifyQueueContextValue>(
    () => ({
      identifyPhases,
      getLibraryPhase,
      queueLibraryIdentify,
    }),
    [getLibraryPhase, identifyPhases, queueLibraryIdentify],
  );

  return <IdentifyQueueContext.Provider value={value}>{children}</IdentifyQueueContext.Provider>;
}

export function useIdentifyQueue() {
  const ctx = useContext(IdentifyQueueContext);
  if (!ctx) throw new Error("useIdentifyQueue must be used within IdentifyQueueProvider");
  return ctx;
}
