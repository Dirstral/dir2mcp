/**
 * MCP JSON-RPC types and helpers for dir2mcp tools.
 * SPEC §15: Hit, Span, search/ask output schemas.
 */

export type Span =
  | { kind: "lines"; start_line: number; end_line: number }
  | { kind: "page"; page: number }
  | { kind: "time"; start_ms: number; end_ms: number };

export interface Hit {
  chunk_id: number;
  rel_path: string;
  doc_type?: string;
  rep_type?: string;
  score?: number;
  snippet: string;
  span: Span;
}

export interface SearchResult {
  query: string;
  k?: number;
  index_used?: string;
  hits: Hit[];
  indexing_complete: boolean;
}

export interface Citation {
  chunk_id: number;
  rel_path: string;
  span: Span;
}

export interface AskResult {
  question: string;
  answer?: string;
  citations: Citation[];
  hits: Hit[];
  indexing_complete: boolean;
}

export interface OpenFileResult {
  rel_path: string;
  doc_type?: string;
  span?: Span;
  content: string;
  truncated: boolean;
}

/** Format span as citation reference: path:L12-L25, path#p=3, path@t=1:23-1:53 */
export function formatCitation(relPath: string, span: Span | undefined): string {
  if (!span || typeof span !== "object") return relPath;
  switch (span.kind) {
    case "lines":
      return `${relPath}:L${span.start_line}-L${span.end_line}`;
    case "page":
      return `${relPath}#p=${span.page}`;
    case "time": {
      const fmt = (ms: number) => {
        const m = Math.floor(ms / 60000);
        const s = Math.floor((ms % 60000) / 1000);
        return `${m}:${String(s).padStart(2, "0")}`;
      };
      return `${relPath}@t=${fmt(span.start_ms)}-${fmt(span.end_ms)}`;
    }
    default:
      return relPath;
  }
}

/** Call MCP tool via /api/mcp proxy. Returns parsed structuredContent or throws. */
export async function mcpCall<T>(
  apiUrl: string,
  toolName: string,
  args: Record<string, unknown>,
  signal?: AbortSignal
): Promise<T> {
  const res = await fetch(`${apiUrl}/api/mcp`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      jsonrpc: "2.0",
      method: "tools/call",
      params: { name: toolName, arguments: args },
      id: 1,
    }),
    signal,
  });
  const text = await res.text();
  if (!res.ok) {
    throw new Error(`Request failed (${res.status})`);
  }
  let data: { error?: { message?: string }; result?: { structuredContent?: T } };
  try {
    data = JSON.parse(text);
  } catch {
    throw new Error("Invalid response");
  }
  if (data.error) {
    throw new Error(data.error.message ?? "Tool call failed");
  }
  const structured = data.result?.structuredContent;
  if (!structured) {
    throw new Error("No results in response");
  }
  return structured as T;
}
