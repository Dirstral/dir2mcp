#!/usr/bin/env python3
"""Thin HTTP bridge: ElevenLabs webhook tools → dir2mcp MCP server.

Translates simple REST calls from ElevenLabs into MCP JSON-RPC tool calls.
Run alongside dir2mcp and expose via the same tunnel.
"""

import json
import os
import threading

import requests
from flask import Flask, jsonify, request

app = Flask(__name__)

# --- Configuration ---
MCP_URL = os.environ.get("MCP_URL", "http://127.0.0.1:8087/mcp")
MCP_TOKEN = os.environ.get("MCP_TOKEN", "")
PROTOCOL_VERSION = "2025-11-25"

# --- Session state ---
_session_id = None
_session_lock = threading.Lock()
_request_id = 0
_id_lock = threading.Lock()


def _next_id():
    global _request_id
    with _id_lock:
        _request_id += 1
        return _request_id


def _ensure_session():
    """Initialize MCP session if not already done."""
    global _session_id
    with _session_lock:
        if _session_id:
            return _session_id
        resp = requests.post(
            MCP_URL,
            headers={
                "Content-Type": "application/json",
                "MCP-Protocol-Version": PROTOCOL_VERSION,
                "Authorization": f"Bearer {MCP_TOKEN}",
            },
            json={
                "jsonrpc": "2.0",
                "id": _next_id(),
                "method": "initialize",
                "params": {
                    "protocolVersion": PROTOCOL_VERSION,
                    "capabilities": {},
                    "clientInfo": {"name": "elevenlabs-bridge", "version": "1.0"},
                },
            },
            timeout=10,
        )
        resp.raise_for_status()
        _session_id = resp.headers.get("Mcp-Session-Id", "")
        return _session_id


def _call_tool(tool_name, arguments):
    """Call an MCP tool and return the result."""
    session_id = _ensure_session()
    resp = requests.post(
        MCP_URL,
        headers={
            "Content-Type": "application/json",
            "MCP-Protocol-Version": PROTOCOL_VERSION,
            "Authorization": f"Bearer {MCP_TOKEN}",
            "MCP-Session-Id": session_id,
        },
        json={
            "jsonrpc": "2.0",
            "id": _next_id(),
            "method": "tools/call",
            "params": {"name": tool_name, "arguments": arguments},
        },
        timeout=30,
    )
    resp.raise_for_status()
    data = resp.json()
    if "error" in data:
        return {"error": data["error"]}, 500
    result = data.get("result", {})
    # Return the text content for simple consumption
    content = result.get("content", [])
    text = "\n".join(c.get("text", "") for c in content if c.get("type") == "text")
    return {"result": text, "structured": result.get("structuredContent")}


# --- Routes ---


@app.route("/health", methods=["GET"])
def health():
    return jsonify({"status": "ok"})


@app.route("/search", methods=["POST"])
def search():
    """Semantic search across indexed documents, returns full source text."""
    body = request.json or {}
    query = body.get("query", "")
    if not query:
        return jsonify({"error": "query is required"}), 400
    result = _call_tool("dir2mcp.search", {"query": query, "k": body.get("k", 3)})
    if isinstance(result, tuple):
        return jsonify(result[0]), result[1]

    # Enrich: fetch full source text for each hit via open_file
    hits = result.get("structured", {}).get("hits", [])
    enriched = []
    for h in hits[:3]:
        rel_path = h.get("RelPath", "")
        span = h.get("Span", {})
        args = {"rel_path": rel_path, "max_chars": 3000}
        if span.get("Kind") == "lines":
            args["start_line"] = span.get("StartLine", 1)
            args["end_line"] = span.get("EndLine", 100)
        source = _call_tool("dir2mcp.open_file", args)
        text = source.get("result", "") if not isinstance(source, tuple) else ""
        enriched.append({"file": rel_path, "score": h.get("Score", 0), "content": text})

    summary = "\n\n".join(
        f"=== {e['file']} (relevance: {e['score']:.0%}) ===\n{e['content']}"
        for e in enriched
    )
    return jsonify({"result": summary})


@app.route("/ask", methods=["POST"])
def ask():
    """Answer a question using search + full source text."""
    body = request.json or {}
    question = body.get("question", "")
    if not question:
        return jsonify({"error": "question is required"}), 400

    # Use search to find relevant docs, then return full content
    # so the ElevenLabs agent LLM can synthesize the answer itself
    result = _call_tool("dir2mcp.search", {"query": question, "k": 3})
    if isinstance(result, tuple):
        return jsonify(result[0]), result[1]

    hits = result.get("structured", {}).get("hits", [])
    enriched = []
    for h in hits[:3]:
        rel_path = h.get("RelPath", "")
        span = h.get("Span", {})
        args = {"rel_path": rel_path, "max_chars": 3000}
        if span.get("Kind") == "lines":
            args["start_line"] = span.get("StartLine", 1)
            args["end_line"] = span.get("EndLine", 100)
        source = _call_tool("dir2mcp.open_file", args)
        text = source.get("result", "") if not isinstance(source, tuple) else ""
        enriched.append({"file": rel_path, "content": text})

    context = "\n\n".join(
        f"=== Source: {e['file']} ===\n{e['content']}" for e in enriched
    )
    return jsonify({
        "result": f"Question: {question}\n\nRelevant document content:\n\n{context}"
    })


@app.route("/list_files", methods=["POST", "GET"])
def list_files():
    """List indexed files."""
    result = _call_tool("dir2mcp.list_files", {})
    if isinstance(result, tuple):
        return jsonify(result[0]), result[1]
    return jsonify(result)


@app.route("/stats", methods=["POST", "GET"])
def stats():
    """Corpus statistics."""
    result = _call_tool("dir2mcp.stats", {})
    if isinstance(result, tuple):
        return jsonify(result[0]), result[1]
    return jsonify(result)


if __name__ == "__main__":
    if not MCP_TOKEN:
        token_path = os.path.join(os.path.dirname(__file__), ".dir2mcp", "secret.token")
        if os.path.exists(token_path):
            MCP_TOKEN = open(token_path).read().strip()
        else:
            print("ERROR: Set MCP_TOKEN or ensure .dir2mcp/secret.token exists")
            raise SystemExit(1)

    print(f"Bridge: {MCP_URL} (token={MCP_TOKEN[:8]}...)")
    print("Endpoints: /search, /ask, /list_files, /stats")
    app.run(host="127.0.0.1", port=8088, debug=False)
