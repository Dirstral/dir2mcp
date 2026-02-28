"use client";

import { useEffect, useState, useCallback } from "react";
import { NavBar } from "@/components/NavBar";

type Corpus = {
  root: string;
  profile?: { doc_counts?: Record<string, number>; code_ratio?: number };
  models?: {
    embed_text?: string;
    embed_code?: string;
    ocr?: string;
    stt_provider?: string;
    stt_model?: string;
    chat?: string;
  };
  indexing: {
    job_id: string;
    running: boolean;
    mode: string;
    scanned: number;
    indexed: number;
    skipped: number;
    deleted: number;
    representations: number;
    chunks_total: number;
    embedded_ok: number;
    errors: number;
  };
};

const POLL_INTERVAL_MS = 2000;

export default function Home() {
  const [corpus, setCorpus] = useState<Corpus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchCorpus = useCallback(async (signal?: AbortSignal | undefined) => {
    try {
      const res = await fetch("/api/corpus", { signal });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error(data.error || res.statusText);
      }
      const data = await res.json();
      setCorpus(data);
      setError(null);
    } catch (e) {
      if (e instanceof Error && e.name === "AbortError") return;
      setError(e instanceof Error ? e.message : "Failed to load");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    fetchCorpus(controller.signal);
    const id = setInterval(() => {
      fetchCorpus(controller.signal);
    }, POLL_INTERVAL_MS);
    return () => {
      clearInterval(id);
      controller.abort();
    };
  }, [fetchCorpus]);

  const docCounts = corpus?.profile?.doc_counts ?? {};
  const totalDocs = Object.values(docCounts).reduce((a, b) => a + b, 0);
  const idx = corpus?.indexing;
  const progress =
    idx && idx.chunks_total > 0
      ? Math.round((idx.embedded_ok / idx.chunks_total) * 100)
      : idx && idx.scanned > 0
        ? Math.min(99, Math.round((idx.indexed / idx.scanned) * 100))
        : 0;

  return (
    <div className="min-h-screen bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100">
      <NavBar />
      <main className="max-w-2xl mx-auto p-6">
        <h1 className="text-2xl font-semibold mb-4">dir2mcp Dashboard</h1>
        {loading && !corpus && (
          <p className="text-zinc-600 dark:text-zinc-400 mb-4">Loading…</p>
        )}
        {error && (
          <p className="text-amber-600 dark:text-amber-400 mb-4">
            {error}
            {error.includes("API_TOKEN") && (
              <span className="block mt-1 text-sm">
                Add API_TOKEN to .env.local (copy from dir2mcp up output).
              </span>
            )}
          </p>
        )}
        {!loading && corpus && (
          <div className="space-y-6 text-sm">
            <div>
              <p><strong>Root:</strong> {corpus.root}</p>
              <p><strong>Mode:</strong> {corpus.indexing?.mode ?? "incremental"}</p>
            </div>

            {idx && (
              <div>
                <strong>Indexing progress</strong>
                <div className="mt-2 h-2 w-full rounded-full bg-zinc-200 dark:bg-zinc-800 overflow-hidden">
                  <div
                    className="h-full bg-blue-500 dark:bg-blue-600 transition-all duration-300"
                    style={{ width: `${progress}%` }}
                  />
                </div>
                <p className="mt-1 text-zinc-500 dark:text-zinc-400">
                  {idx.running ? "Running" : "Idle"} · scanned: {idx.scanned} · indexed: {idx.indexed} · embedded: {idx.embedded_ok}
                  {idx.chunks_total > 0 && ` / ${idx.chunks_total}`}
                </p>
              </div>
            )}

            {totalDocs > 0 && (
              <div>
                <strong>Doc type breakdown</strong>
                <div className="mt-2 space-y-1">
                  {Object.entries(docCounts)
                    .sort(([, a], [, b]) => b - a)
                    .map(([type, count]) => (
                      <div key={type} className="flex items-center gap-2">
                        <div
                          className="h-2 rounded bg-zinc-300 dark:bg-zinc-600 min-w-[4rem]"
                          style={{
                            width: `${Math.max(4, (count / totalDocs) * 100)}%`,
                          }}
                        />
                        <span className="font-mono text-xs">{type}</span>
                        <span className="text-zinc-500 dark:text-zinc-400">{count}</span>
                      </div>
                    ))}
                </div>
              </div>
            )}

            {corpus.models && (
              <div>
                <strong>Models</strong>
                <ul className="list-disc list-inside mt-1 text-zinc-600 dark:text-zinc-400">
                  <li>embed_text: {corpus.models.embed_text ?? "—"}</li>
                  <li>embed_code: {corpus.models.embed_code ?? "—"}</li>
                  <li>ocr: {corpus.models.ocr ?? "—"}</li>
                  <li>stt: {corpus.models.stt_provider ?? "—"} / {corpus.models.stt_model ?? "—"}</li>
                  <li>chat: {corpus.models.chat ?? "—"}</li>
                </ul>
              </div>
            )}

            <div>
              <strong>Indexing details</strong>
              <ul className="mt-1 text-zinc-600 dark:text-zinc-400">
                <li>job_id: {idx?.job_id ?? "—"}</li>
                <li>running: {String(idx?.running ?? false)}</li>
                <li>scanned: {idx?.scanned ?? 0} | indexed: {idx?.indexed ?? 0} | skipped: {idx?.skipped ?? 0}</li>
                <li>representations: {idx?.representations ?? 0} | chunks_total: {idx?.chunks_total ?? 0} | embedded_ok: {idx?.embedded_ok ?? 0}</li>
                <li>errors: {idx?.errors ?? 0}</li>
              </ul>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
