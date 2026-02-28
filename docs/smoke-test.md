# MCP Endpoint Smoke Test Checklist

Run these curl commands against your endpoint to verify it works before connecting ElevenLabs.

Replace `<URL>` with your MCP URL and `<TOKEN>` with your bearer token.

```bash
URL="http://localhost:8080/mcp"          # or ngrok URL
TOKEN="$(cat .dir2mcp/secret.token)"
```

## 1. Initialize (handshake)

```bash
curl -s -D- -X POST "$URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2025-11-25",
      "capabilities": {"tools": {}},
      "clientInfo": {"name": "smoke-test", "version": "1.0"}
    }
  }'
```

**Expected:** HTTP 200 with `MCP-Session-Id` header and JSON body containing `protocolVersion`, `capabilities`, `serverInfo`.

**Save the session ID for subsequent requests:**

```bash
SESSION_ID="<value from MCP-Session-Id header>"
```

## 2. Tools List (discover tools)

```bash
curl -s -X POST "$URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "MCP-Session-Id: $SESSION_ID" \
  -d '{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}}' | python3 -m json.tool
```

**Expected:** JSON with `result.tools` array containing 5 tools: `dir2mcp.search`, `dir2mcp.ask`, `dir2mcp.open_file`, `dir2mcp.list_files`, `dir2mcp.stats`.

## 3. Tools Call: list_files

```bash
curl -s -X POST "$URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "MCP-Session-Id: $SESSION_ID" \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {"name": "dir2mcp.list_files", "arguments": {"limit": 5}}
  }' | python3 -m json.tool
```

**Expected:** JSON with `result.structuredContent.files` array.

## 4. Tools Call: search

```bash
curl -s -X POST "$URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "MCP-Session-Id: $SESSION_ID" \
  -d '{
    "jsonrpc": "2.0",
    "id": 4,
    "method": "tools/call",
    "params": {"name": "dir2mcp.search", "arguments": {"query": "hello world", "k": 3}}
  }' | python3 -m json.tool
```

**Expected:** JSON with `result.structuredContent.hits` array (may be empty if index is not yet built).

## 5. Tools Call: stats

```bash
curl -s -X POST "$URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -H "MCP-Session-Id: $SESSION_ID" \
  -d '{
    "jsonrpc": "2.0",
    "id": 5,
    "method": "tools/call",
    "params": {"name": "dir2mcp.stats", "arguments": {}}
  }' | python3 -m json.tool
```

**Expected:** JSON with `result.structuredContent.root` and `result.structuredContent.protocol_version`.

## 6. Auth rejection (no token)

```bash
curl -s -o /dev/null -w "%{http_code}" -X POST "$URL" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 6, "method": "initialize", "params": {}}'
```

**Expected:** HTTP `401`.

## 7. CORS preflight (OPTIONS)

```bash
curl -s -D- -X OPTIONS "$URL" \
  -H "Origin: https://elevenlabs.io" \
  -H "Access-Control-Request-Method: POST" \
  -H "Access-Control-Request-Headers: Content-Type, Authorization"
```

**Expected:** HTTP 204 with `Access-Control-Allow-Origin: https://elevenlabs.io` (only if origin is in the allowlist).

## Quick one-liner check

```bash
curl -sf -X POST "$URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"clientInfo":{"name":"check","version":"1"}}}' \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('OK' if d.get('result',{}).get('protocolVersion')=='2025-11-25' else 'FAIL')"
```
