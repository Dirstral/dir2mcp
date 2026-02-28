import { NextResponse } from "next/server";

/**
 * Proxies /api/corpus from the dir2mcp backend.
 * Requires API_TOKEN (server-only) so the bearer token never reaches the browser.
 */
export async function GET() {
  const apiUrl = process.env.NEXT_PUBLIC_API_URL;
  const token = process.env.API_TOKEN;
  if (!apiUrl) {
    return NextResponse.json(
      { error: "NEXT_PUBLIC_API_URL not configured" },
      { status: 500 }
    );
  }
  if (!token) {
    return NextResponse.json(
      { error: "API_TOKEN not configured" },
      { status: 500 }
    );
  }
  try {
    const res = await fetch(`${apiUrl}/api/corpus`, {
      headers: { Authorization: `Bearer ${token}` },
      next: { revalidate: 0 },
      signal: AbortSignal.timeout(10000),
    });
    if (!res.ok) {
      return NextResponse.json(
        { error: "Corpus unavailable" },
        { status: res.status >= 500 ? 502 : res.status }
      );
    }
    const data = await res.json();
    return NextResponse.json(data);
  } catch {
    return NextResponse.json(
      { error: "Service unavailable" },
      { status: 502 }
    );
  }
}
