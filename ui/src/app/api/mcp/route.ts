import { NextRequest, NextResponse } from "next/server";

/**
 * Proxies MCP tool calls to the dir2mcp backend.
 * Requires API_TOKEN (server-only) so the bearer token never reaches the browser.
 */
export async function POST(request: NextRequest) {
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
    const body = await request.text();
    const res = await fetch(`${apiUrl}/api/mcp`, {
      method: "POST",
      headers: {
        "Content-Type": request.headers.get("Content-Type") || "application/json",
        Authorization: `Bearer ${token}`,
      },
      body,
      signal: AbortSignal.timeout(30000),
    });
    const data = await res.text();
    return new NextResponse(data, {
      status: res.status,
      headers: { "Content-Type": res.headers.get("Content-Type") || "application/json" },
    });
  } catch {
    return NextResponse.json(
      { error: "Service unavailable" },
      { status: 502 }
    );
  }
}
