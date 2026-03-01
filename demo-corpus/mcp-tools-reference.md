# dir2mcp MCP Tools Reference

## Available Tools

### dir2mcp.search

Performs semantic search across indexed documents. Returns ranked results with relevance scores and source citations.

**Parameters:**
- `query` (string, required): The search query in natural language
- `k` (integer, 1-50, default 10): Number of results to return
- `index` (string, "text" or "code"): Which index to search
- `path_prefix` (string): Filter results to paths starting with this prefix
- `glob` (string): Filter results by glob pattern
- `doc_types` (array): Filter by document types

**Example:**
```json
{
  "query": "How does the vector index work?",
  "k": 5,
  "index": "text"
}
```

### dir2mcp.ask

Generates a RAG-based answer with citations. Combines semantic search with LLM generation to produce grounded answers.

**Parameters:**
- `question` (string, required): The question to answer
- `k` (integer, 1-50, default 10): Number of search results to use as context
- Other filters: `index`, `path_prefix`, `glob`, `doc_types`

**Example:**
```json
{
  "question": "What document types are supported for indexing?",
  "k": 5
}
```

### dir2mcp.open_file

Retrieves the exact source text from an indexed file. Useful for verifying citations or reading specific sections.

**Parameters:**
- `rel_path` (string, required): Relative path of the file within the indexed directory
- `span` (object): Line or page range to extract
- `max_chars` (integer): Maximum characters to return

### dir2mcp.list_files

Lists all indexed files with metadata including file size, modification time, indexing status, and document type.

**Parameters:**
- `path_prefix` (string): Filter by path prefix
- `glob` (string): Filter by glob pattern
- `limit` (integer, 1-5000): Maximum files to return
- `offset` (integer): Pagination offset

### dir2mcp.stats

Returns corpus-level statistics including total files indexed, chunk counts, embedding model information, and indexing progress.

**Parameters:** None

## Usage Notes

- All tools are read-only and safe to auto-approve
- Search results include citation spans (line numbers, page numbers, or timestamps) for source verification
- The `ask` tool automatically retrieves relevant context before generating an answer
- Rate limiting applies: 60 requests/second with burst of 20 by default
