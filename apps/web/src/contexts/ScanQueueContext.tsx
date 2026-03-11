import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  getLibraryScanStatus as fetchLibraryScanStatus,
  startLibraryScan,
  type LibraryScanStatus,
} from "../api";
import { queryKeys, useLibraries } from "../queries";

type QueueScanOptions = {
  identify?: boolean;
};

type ScanQueueContextValue = {
  scanStatuses: Record<number, LibraryScanStatus>;
  getLibraryScanStatus: (libraryId: number | null) => LibraryScanStatus | undefined;
  queueLibraryScan: (libraryId: number, options?: QueueScanOptions) => Promise<LibraryScanStatus>;
};

const SCAN_POLL_INTERVAL_MS = 2_000;

const ScanQueueContext = createContext<ScanQueueContextValue | null>(null);

function isActiveScan(phase?: string) {
  return phase === "queued" || phase === "scanning";
}

function isLibraryProcessing(status?: LibraryScanStatus) {
  return (
    status != null &&
    (isActiveScan(status.phase) ||
      status.enriching ||
      status.identifyPhase === "queued" ||
      status.identifyPhase === "identifying")
  );
}

export function ScanQueueProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const { data: libraries = [] } = useLibraries();
  const [scanStatuses, setScanStatuses] = useState<Record<number, LibraryScanStatus>>({});

  const setScanStatus = useCallback((status: LibraryScanStatus) => {
    setScanStatuses((current) => {
      const previous = current[status.libraryId];
      if (
        previous &&
        previous.phase === status.phase &&
        previous.enriching === status.enriching &&
        previous.identifyPhase === status.identifyPhase &&
        previous.identified === status.identified &&
        previous.identifyFailed === status.identifyFailed &&
        previous.processed === status.processed &&
        previous.added === status.added &&
        previous.updated === status.updated &&
        previous.removed === status.removed &&
        previous.unmatched === status.unmatched &&
        previous.skipped === status.skipped &&
        previous.error === status.error &&
        previous.finishedAt === status.finishedAt &&
        previous.startedAt === status.startedAt &&
        previous.identifyRequested === status.identifyRequested
      ) {
        return current;
      }
      return { ...current, [status.libraryId]: status };
    });
  }, []);

  const refreshLibraryScanStatus = useCallback(
    async (libraryId: number) => {
      const status = await fetchLibraryScanStatus(libraryId);
      setScanStatus(status);
      if (isLibraryProcessing(status) || status.phase === "completed" || status.phase === "failed") {
        void queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) });
        void queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
      }
      return status;
    },
    [queryClient, setScanStatus],
  );

  const queueLibraryScan = useCallback(
    async (libraryId: number, options?: QueueScanOptions) => {
      const status = await startLibraryScan(libraryId, options);
      setScanStatus(status);
      void queryClient.invalidateQueries({ queryKey: queryKeys.library(libraryId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.libraries });
      return status;
    },
    [queryClient, setScanStatus],
  );

  const getLibraryScanStatus = useCallback(
    (libraryId: number | null) => (libraryId == null ? undefined : scanStatuses[libraryId]),
    [scanStatuses],
  );

  useEffect(() => {
    const activeLibraryIds = new Set(libraries.map((library) => library.id));
    setScanStatuses((current) => {
      const nextEntries = Object.entries(current).filter(([libraryId]) =>
        activeLibraryIds.has(parseInt(libraryId, 10)),
      );
      return nextEntries.length === Object.keys(current).length
        ? current
        : Object.fromEntries(nextEntries);
    });

    if (libraries.length === 0) return;
    void Promise.allSettled(libraries.map((library) => refreshLibraryScanStatus(library.id)));
  }, [libraries, refreshLibraryScanStatus]);

  useEffect(() => {
    const activeScanIds = Object.values(scanStatuses)
      .filter((status) => isLibraryProcessing(status))
      .map((status) => status.libraryId);
    if (activeScanIds.length === 0) return;

    const intervalId = window.setInterval(() => {
      void Promise.allSettled(activeScanIds.map((libraryId) => refreshLibraryScanStatus(libraryId)));
    }, SCAN_POLL_INTERVAL_MS);
    return () => window.clearInterval(intervalId);
  }, [refreshLibraryScanStatus, scanStatuses]);

  const value = useMemo<ScanQueueContextValue>(
    () => ({
      scanStatuses,
      getLibraryScanStatus,
      queueLibraryScan,
    }),
    [getLibraryScanStatus, queueLibraryScan, scanStatuses],
  );

  return <ScanQueueContext.Provider value={value}>{children}</ScanQueueContext.Provider>;
}

export function useScanQueue() {
  const ctx = useContext(ScanQueueContext);
  if (!ctx) throw new Error("useScanQueue must be used within ScanQueueProvider");
  return ctx;
}
