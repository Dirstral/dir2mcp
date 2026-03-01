# dir2mcp Architecture Overview

## Summary

dir2mcp is a deploy-first MCP server that turns any local directory into an AI-accessible knowledge base. It indexes documents incrementally, generates vector embeddings, and exposes semantic search and RAG capabilities via the Model Context Protocol.

## Core Components

### Ingestion Pipeline

The ingestion pipeline discovers files, classifies document types, extracts text representations, chunks content, and generates vector embeddings. It runs in the background during server operation.

Supported document types:
- **Text files**: Markdown, plain text, configuration files
- **Code files**: Go, Python, JavaScript, TypeScript, and more
- **PDFs**: Converted via Mistral OCR API
- **Images**: Analyzed via Mistral vision models
- **Audio**: Transcribed via Mistral STT (Voxtral)

### Vector Index

Uses HNSW (Hierarchical Navigable Small World) approximate nearest neighbor search. Two separate indexes are maintained:
- `vectors_text.hnsw` — for text/document chunks (Mistral Embed model)
- `vectors_code.hnsw` — for code chunks (Codestral Embed model)

### MCP Server

Implements the MCP 2025-11-25 specification with Streamable HTTP transport. Features include:
- Session management with 24-hour TTL
- Bearer token authentication
- CORS support for cross-origin clients
- Per-IP rate limiting

## Deployment

The server can run locally for development or be exposed publicly via tunnels (ngrok, Cloudflare Tunnel). Public mode enforces authentication by default.

## Data Flow

```
Files on disk → Discover → Classify → Extract → Chunk → Embed → HNSW Index
                                                                       ↓
User query → Embed query → KNN search → Retrieve chunks → Generate answer
```
