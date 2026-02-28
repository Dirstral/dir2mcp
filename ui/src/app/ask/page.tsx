"use client";

import { useState, useRef, useEffect, useCallback } from "react";
import { NavBar } from "@/components/NavBar";
import { DocTypeBadge } from "@/components/DocTypeBadge";
import { mcpCall, formatCitation, type Hit, type AskResult } from "@/lib/mcp";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "";

export default function AskPage() {
  const [question, setQuestion] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<AskResult | null>(null);
  const [sourcesOpen, setSourcesOpen] = useState(false);
  const abortRef = useRef<AbortController | null>(null);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (!question.trim()) return;
      if (!API_URL) {
        setError("Set NEXT_PUBLIC_API_URL to your dir2mcp up URL.");
        return;
      }
      abortRef.current?.abort();
      abortRef.current = new AbortController();
      setError(null);
      setResult(null);
      setLoading(true);
      try {
        const data = await mcpCall<AskResult>(
          API_URL,
          "dir2mcp.ask",
          { question: question.trim(), k: 10, mode: "answer" },
          abortRef.current.signal
        );
        setResult(data);
        setSourcesOpen(false);
      } catch (e) {
        if (e instanceof Error && e.name === "AbortError") return;
        setError(e instanceof Error ? e.message : "Request failed");
      } finally {
        setLoading(false);
      }
    },
    [question]
  );

  useEffect(() => {
    return () => abortRef.current?.abort();
  }, []);

  return (
    <div className="min-h-screen bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100">
      <NavBar />
      <main className="max-w-2xl mx-auto p-6">
        <h1 className="text-2xl font-semibold mb-4">Ask</h1>
        <p className="text-zinc-600 dark:text-zinc-400 mb-4">
          Ask a question and get an answer with citations via the MCP{" "}
          <code className="bg-zinc-200 dark:bg-zinc-800 px-1 rounded">dir2mcp.ask</code> tool.
        </p>
        <form onSubmit={handleSubmit} className="space-y-3">
          <label htmlFor="ask-question" className="sr-only">
            Your question
          </label>
          <textarea
            id="ask-question"
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            placeholder="Your question..."
            rows={4}
            aria-label="Your question"
            className="w-full rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 px-3 py-2 text-zinc-900 dark:text-zinc-100 resize-y"
          />
          <button
            type="submit"
            disabled={loading || !question.trim()}
            className="rounded bg-zinc-900 dark:bg-zinc-100 text-zinc-50 dark:text-zinc-900 px-4 py-2 font-medium hover:opacity-90 disabled:opacity-50"
          >
            {loading ? "Asking…" : "Ask"}
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
                Indexing still in progress. Answer may be incomplete.
              </div>
            )}

            {result.answer && (
              <div className="rounded-lg border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-4">
                <h2 className="text-sm font-semibold text-zinc-500 dark:text-zinc-400 mb-2">
                  Answer
                </h2>
                <div className="prose prose-sm dark:prose-invert max-w-none">
                  {result.answer.split(/(\[[^\]]+\])/g).map((part, i) => {
                    const m = part.match(/^\[([^\]]+)\]$/);
                    if (m) {
                      const ref = m[1];
                      return (
                        <span
                          key={`cite-${i}`}
                          className="inline rounded bg-blue-100 dark:bg-blue-900/40 px-1 py-0.5 text-blue-800 dark:text-blue-200 text-sm font-medium"
                          title={ref}
                        >
                          [{ref}]
                        </span>
                      );
                    }
                    return <span key={`text-${i}`}>{part}</span>;
                  })}
                </div>
              </div>
            )}

            {result.hits.length > 0 && (
              <div className="rounded-lg border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 overflow-hidden">
                <button
                  type="button"
                  onClick={() => setSourcesOpen(!sourcesOpen)}
                  className="w-full flex items-center justify-between px-4 py-3 text-left font-medium hover:bg-zinc-50 dark:hover:bg-zinc-800/50"
                  aria-expanded={sourcesOpen}
                  aria-controls="sources-panel"
                >
                  Sources ({result.hits.length})
                  <span className="text-zinc-500 dark:text-zinc-400" aria-hidden>
                    {sourcesOpen ? "▼" : "▶"}
                  </span>
                </button>
                {sourcesOpen && (
                  <div
                    id="sources-panel"
                    className="border-t border-zinc-200 dark:border-zinc-800 px-4 py-3 space-y-3 max-h-60 overflow-y-auto"
                  >
                    {result.hits.map((hit, i) => (
                      <div
                        key={`${hit.chunk_id}-${i}`}
                        className="text-sm border-b border-zinc-100 dark:border-zinc-800 last:border-0 pb-3 last:pb-0"
                      >
                        <div className="flex flex-wrap items-center gap-2 mb-1">
                          <span className="font-mono text-xs font-medium text-zinc-700 dark:text-zinc-300">
                            {hit.rel_path}
                          </span>
                          <DocTypeBadge docType={hit.doc_type} />
                          <span className="text-xs text-zinc-500 dark:text-zinc-400">
                            {formatCitation(hit.rel_path, hit.span)}
                          </span>
                        </div>
                        <p className="text-zinc-600 dark:text-zinc-400 line-clamp-2">
                          {hit.snippet}
                        </p>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </main>
    </div>
  );
}
