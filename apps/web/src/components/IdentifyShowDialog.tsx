import { useCallback, useEffect, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "./ui/dialog";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { useIdentifyQueue } from "../contexts/IdentifyQueueContext";
import { identifyShow, searchSeries, type SeriesSearchResult } from "../api";

export interface IdentifyShowDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  libraryId: number;
  showKey: string;
  showTitle: string;
  onSuccess: () => void;
}

export function IdentifyShowDialog({
  open,
  onOpenChange,
  libraryId,
  showKey,
  showTitle,
  onSuccess,
}: IdentifyShowDialogProps) {
  const { queueLibraryIdentify } = useIdentifyQueue();
  const [query, setQuery] = useState(showTitle);
  const [results, setResults] = useState<readonly SeriesSearchResult[]>([]);
  const [loading, setLoading] = useState(false);
  const [identifying, setIdentifying] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setQuery(showTitle);
      setResults([]);
      setError(null);
    }
  }, [open, showTitle]);

  const doSearch = useCallback(async () => {
    const q = query.trim();
    if (!q) {
      setResults([]);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const data = await searchSeries(q);
      setResults(data);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Search failed");
      setResults([]);
    } finally {
      setLoading(false);
    }
  }, [query]);

  useEffect(() => {
    if (!open || !query.trim()) return;
    const t = setTimeout(doSearch, 300);
    return () => clearTimeout(t);
  }, [open, query, doSearch]);

  async function handleChoose(result: SeriesSearchResult) {
    const id = result.ExternalID;
    setIdentifying(id);
    setError(null);
    try {
      const response = await identifyShow(libraryId, showKey, parseInt(id, 10));
      if (response.updated <= 0) {
        setError("Identify failed");
        return;
      }
      queueLibraryIdentify(libraryId, {
        abortActive: true,
        prioritize: true,
        resetState: true,
      });
      onSuccess();
      onOpenChange(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Identify failed");
    } finally {
      setIdentifying(null);
    }
  }

  const year = (r: SeriesSearchResult) =>
    r.ReleaseDate ? new Date(r.ReleaseDate).getFullYear() : "";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent onClose={() => onOpenChange(false)}>
        <DialogHeader>
          <DialogTitle>Identify show</DialogTitle>
        </DialogHeader>
        <DialogDescription>
          Search for the correct series and choose it to update this show&apos;s metadata.
        </DialogDescription>
        <div className="flex gap-2">
          <Input
            type="search"
            placeholder="Search for a TV series…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && doSearch()}
            className="flex-1"
          />
          <Button type="button" variant="outline" onClick={doSearch} disabled={loading}>
            {loading ? "Searching…" : "Search"}
          </Button>
        </div>
        {error && (
          <p className="text-sm text-red-500" role="alert">
            {error}
          </p>
        )}
        <div className="max-h-[60vh] overflow-y-auto space-y-2">
          {results.length === 0 && !loading && query.trim() && !error && (
            <p className="text-sm text-(--plum-muted)">No results.</p>
          )}
          {results.map((r) => (
            <div
              key={r.ExternalID}
              className="flex items-center gap-3 rounded-md border border-(--plum-border) p-2 bg-(--plum-panel)"
            >
              <img
                src={r.PosterURL || "/placeholder-poster.svg"}
                alt=""
                className="w-12 h-18 object-cover rounded-sm"
              />
              <div className="flex-1 min-w-0">
                <div className="font-medium truncate">{r.Title}</div>
                {year(r) && <div className="text-sm text-(--plum-muted)">{year(r)}</div>}
              </div>
              <Button
                type="button"
                size="sm"
                onClick={() => handleChoose(r)}
                disabled={identifying !== null}
              >
                {identifying === r.ExternalID ? "Updating…" : "Choose"}
              </Button>
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
