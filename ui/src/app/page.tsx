"use client";

import { useEffect, useState } from "react";
import { NavBar } from "@/components/NavBar";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "";

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

export default function Home() {
  const [corpus, setCorpus] = useState<Corpus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!API_URL) {
      setError("Set NEXT_PUBLIC_API_URL to your dir2mcp up URL (e.g. http://127.0.0.1:52143)");
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    fetch(`${API_URL}/api/corpus`, { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(res.statusText);
        return res.json();
      })
      .then(setCorpus)
      .catch((e) => {
        if (e.name !== "AbortError") setError(e.message);
      })
      .finally(() => setLoading(false));
    return () => controller.abort();
  }, []);

  return (
    <div className="min-h-screen bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100">
      <NavBar />
      <main className="max-w-2xl mx-auto p-6">
        <h1 className="text-2xl font-semibold mb-4">dir2mcp Dashboard</h1>
        {loading && <p className="text-zinc-600 dark:text-zinc-400 mb-4">Loading…</p>}
        {error && (
          <p className="text-amber-600 dark:text-amber-400 mb-4">{error}</p>
        )}
        {!loading && corpus && (
          <div className="space-y-4 text-sm">
            <p><strong>Root:</strong> {corpus.root}</p>
            <p><strong>Mode:</strong> {corpus.indexing?.mode || "incremental"}</p>
            {corpus.models && (
              <div>
                <strong>Models:</strong>
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
              <strong>Indexing:</strong>
              <ul className="mt-1 text-zinc-600 dark:text-zinc-400">
                <li>job_id: {corpus.indexing.job_id}</li>
                <li>running: {String(corpus.indexing.running)}</li>
                <li>scanned: {corpus.indexing.scanned} | indexed: {corpus.indexing.indexed} | skipped: {corpus.indexing.skipped}</li>
                <li>representations: {corpus.indexing.representations} | chunks_total: {corpus.indexing.chunks_total} | embedded_ok: {corpus.indexing.embedded_ok}</li>
                <li>errors: {corpus.indexing.errors}</li>
              </ul>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}
