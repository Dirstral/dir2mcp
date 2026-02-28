"use client";

import { useState, useRef, useEffect, useCallback } from "react";
import { NavBar } from "@/components/NavBar";
import { DocTypeBadge } from "@/components/DocTypeBadge";
import {
  mcpCall,
  formatCitation,
  type Hit,
  type SearchResult,
  type OpenFileResult,
} from "@/lib/mcp";

// Use "" for proxy mode (client calls Next.js /api/mcp which proxies to dir2mcp)
const MCP_BASE = "";

export default function SearchPage() {
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<SearchResult | null>(null);
  const [sourceModal, setSourceModal] = useState<{ hit: Hit; content: string } | null>(null);
  const [sourceLoading, setSourceLoading] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);

  const fetchSource = useCallback(async (hit: Hit) => {
    setSourceLoading(true);
    try {
      const args: Record<string, unknown> = { rel_path: hit.rel_path };
      const span = hit.span;
      if (span?.kind === "lines") {
        args.start_line = span.start_line;
        args.end_line = span.end_line;
      } else if (span?.kind === "page") {
        args.page = span.page;
      } else if (span?.kind === "time") {
        args.start_ms = span.start_ms;
        args.end_ms = span.end_ms;
      }
      const data = await mcpCall<OpenFileResult>(MCP_BASE, "dir2mcp.open_file", args);
      setSourceModal({ hit, content: data.content });
    } catch (e) {
      setSourceModal({
        hit,
        content: `Error: ${e instanceof Error ? e.message : "Failed to load"}`,
      });
    } finally {
      setSourceLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!sourceModal) return;
    closeButtonRef.current?.focus();
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setSourceModal(null);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [sourceModal]);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (!query.trim()) return;
      const controller = new AbortController();
      abortRef.current?.abort();
      abortRef.current = controller;
      setError(null);
      setResult(null);
      setLoading(true);
      try {
        const data = await mcpCall<SearchResult>(
          MCP_BASE,
          "dir2mcp.search",
          { query: query.trim(), k: 10 },
          controller.signal
        );
        setResult(data);
      } catch (e) {
        if (e instanceof Error && e.name === "AbortError") return;
        setError(e instanceof Error ? e.message : "Request failed");
      } finally {
        if (abortRef.current === controller) setLoading(false);
      }
    },
    [query]
  );

  useEffect(() => {
    return () => abortRef.current?.abort();
  }, []);

  return (
    <div className="min-h-screen bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100">
      <NavBar />
      <main className="max-w-2xl mx-auto p-6">
        <h1 className="text-2xl font-semibold mb-4">Search</h1>
        <p className="text-zinc-600 dark:text-zinc-400 mb-4">
          Query the indexed corpus via the MCP{" "}
          <code className="bg-zinc-200 dark:bg-zinc-800 px-1 rounded">dir2mcp.search</code> tool.
        </p>
        <form onSubmit={handleSubmit} className="space-y-3">
          <label htmlFor="search-query" className="sr-only">
            Search query
          </label>
          <input
            id="search-query"
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search query..."
            aria-label="Search query"
            className="w-full rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 px-3 py-2"
          />
          <button
            type="submit"
            disabled={loading || !query.trim()}
            className="rounded bg-zinc-900 dark:bg-zinc-100 text-zinc-50 dark:text-zinc-900 px-4 py-2 font-medium disabled:opacity-50"
          >
            {loading ? "Searching…" : "Search"}
          </button>
        </form>

        {error && (
          <p className="mt-4 text-amber-600 dark:text-amber-400 text-sm">{error}</p>
        )}

        {result && (
          <div className="mt-6 space-y-4">
            {!result.indexing_complete && (
              <div
                className="rounded border border-amber-200 dark:border-amber-800 bg-amber-50 dark:bg-amber-950/30 px-3 py-2 text-sm text-amber-800 dark:text-amber-200"
                role="status"
              >
                Indexing still in progress. Results may be incomplete.
              </div>
            )}
            <p className="text-sm text-zinc-500 dark:text-zinc-400">
              {result.hits.length} hit{result.hits.length !== 1 ? "s" : ""} for &quot;{result.query}&quot;
            </p>
            <div className="space-y-3">
              {result.hits.map((hit, i) => (
                <article
                  key={`${hit.chunk_id}-${i}`}
                  className="rounded-lg border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-4 shadow-sm"
                >
                  <div className="flex flex-wrap items-center gap-2 mb-2">
                    <span className="font-mono text-sm font-medium text-zinc-700 dark:text-zinc-300">
                      {hit.rel_path}
                    </span>
                    <DocTypeBadge docType={hit.doc_type} />
                    {hit.score != null && (
                      <span className="text-xs text-zinc-500 dark:text-zinc-400">
                        score: {hit.score.toFixed(2)}
                      </span>
                    )}
                  </div>
                  <p className="text-sm text-zinc-600 dark:text-zinc-400 mb-2 line-clamp-3">
                    {hit.snippet}
                  </p>
                  <button
                    type="button"
                    onClick={() => fetchSource(hit)}
                    disabled={sourceLoading}
                    className="text-sm text-blue-600 dark:text-blue-400 hover:underline disabled:opacity-50"
                    aria-label={`View source: ${formatCitation(hit.rel_path, hit.span)}`}
                  >
                    {formatCitation(hit.rel_path, hit.span)} →
                  </button>
                </article>
              ))}
            </div>
          </div>
        )}
      </main>

      {sourceModal && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          onClick={() => setSourceModal(null)}
          role="dialog"
          aria-modal="true"
          aria-labelledby="source-modal-title"
          aria-describedby="source-modal-content"
        >
          <div
            className="max-h-[80vh] w-full max-w-2xl overflow-auto rounded-lg border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-4 shadow-xl"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 id="source-modal-title" className="text-lg font-semibold mb-2">
              {sourceModal.hit.rel_path}
            </h2>
            <pre
              id="source-modal-content"
              className="whitespace-pre-wrap text-sm font-mono bg-zinc-100 dark:bg-zinc-800 rounded p-3 overflow-x-auto"
            >
              {sourceModal.content}
            </pre>
            {sourceModal.content.startsWith("Error:") && (
              <p className="mt-2 text-sm text-amber-600 dark:text-amber-400">
                The MCP server may not implement open_file yet.
              </p>
            )}
            <button
              ref={closeButtonRef}
              type="button"
              onClick={() => setSourceModal(null)}
              className="mt-4 rounded bg-zinc-200 dark:bg-zinc-700 px-3 py-1.5 text-sm hover:bg-zinc-300 dark:hover:bg-zinc-600"
              aria-label="Close modal"
            >
              Close
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
