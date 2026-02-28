"use client";

import { useState } from "react";
import { NavBar } from "@/components/NavBar";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "";

export default function AskPage() {
  const [question, setQuestion] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!API_URL) {
      setMessage("Set NEXT_PUBLIC_API_URL to your dir2mcp up URL.");
      return;
    }
    setMessage(null);
    setLoading(true);
    try {
      const body = {
        jsonrpc: "2.0",
        method: "tools/call",
        params: { name: "dir2mcp.ask", arguments: { question } },
        id: 1,
      };
      const res = await fetch(`${API_URL}/api/mcp`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const text = await res.text();
      if (!res.ok) {
        setMessage(`Server ${res.status}: ${text || res.statusText}`);
        return;
      }
      try {
        const data = JSON.parse(text);
        if (data.error) {
          setMessage(`Error: ${data.error.message || JSON.stringify(data.error)}`);
          return;
        }
        if (data.result?.content) {
          setMessage(`Answer: ${JSON.stringify(data.result.content).slice(0, 600)}...`);
          return;
        }
        setMessage(text.slice(0, 400) + (text.length > 400 ? "..." : ""));
      } catch {
        setMessage(text.slice(0, 400) + (text.length > 400 ? "..." : ""));
      }
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "Request failed");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100">
      <NavBar />
      <main className="max-w-2xl mx-auto p-6">
        <h1 className="text-2xl font-semibold mb-4">Ask</h1>
        <p className="text-zinc-600 dark:text-zinc-400 mb-4">
          Ask a question and get an answer with citations via the MCP <code className="bg-zinc-200 dark:bg-zinc-800 px-1 rounded">dir2mcp.ask</code> tool.
        </p>
        <form onSubmit={handleSubmit} className="space-y-3">
          <label htmlFor="ask-question" className="sr-only">Your question</label>
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
            disabled={loading}
            className="rounded bg-zinc-900 dark:bg-zinc-100 text-zinc-50 dark:text-zinc-900 px-4 py-2 font-medium hover:opacity-90 disabled:opacity-50"
          >
            {loading ? "Asking…" : "Ask"}
          </button>
        </form>
        {message && (
          <p className="mt-4 text-sm text-zinc-600 dark:text-zinc-400">{message}</p>
        )}
      </main>
    </div>
  );
}
