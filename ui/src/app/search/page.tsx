"use client";

import { useState } from "react";
import Link from "next/link";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "";

export default function SearchPage() {
  const [query, setQuery] = useState("");
  const [message, setMessage] = useState<string | null>(null);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!API_URL) {
      setMessage("Set NEXT_PUBLIC_API_URL to your dir2mcp up URL.");
      return;
    }
    setMessage("Search via MCP (dir2mcp.search) will be available when the tool is implemented.");
  };

  return (
    <div className="min-h-screen bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100">
      <nav className="border-b border-zinc-200 dark:border-zinc-800 px-4 py-3 flex gap-4">
        <Link href="/" className="font-medium hover:underline">Dashboard</Link>
        <Link href="/search" className="font-medium hover:underline">Search</Link>
        <Link href="/ask" className="font-medium hover:underline">Ask</Link>
      </nav>
      <main className="max-w-2xl mx-auto p-6">
        <h1 className="text-2xl font-semibold mb-4">Search</h1>
        <p className="text-zinc-600 dark:text-zinc-400 mb-4">
          Query the indexed corpus via the MCP dir2mcp.search tool.
        </p>
        <form onSubmit={handleSubmit} className="space-y-3">
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search query..."
            className="w-full rounded border border-zinc-300 dark:border-zinc-700 bg-white dark:bg-zinc-900 px-3 py-2"
          />
          <button
            type="submit"
            className="rounded bg-zinc-900 dark:bg-zinc-100 text-zinc-50 dark:text-zinc-900 px-4 py-2 font-medium"
          >
            Search
          </button>
        </form>
        {message && <p className="mt-4 text-sm text-zinc-600 dark:text-zinc-400">{message}</p>}
      </main>
    </div>
  );
}
