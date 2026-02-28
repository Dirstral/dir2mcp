package ingest

import (
	"path/filepath"
	"strings"
)

// ClassifyDocType maps a path to an ingestion document type.
func ClassifyDocType(relPath string) string {
	base := strings.ToLower(filepath.Base(relPath))

	// treat plain ".env" and dot-separated variants as sensitive and
	// skip them during ingestion. these often contain secrets/credentials
	// so we classify them as "ignore". previously they were marked as
	// "data" which risked accidental indexing; other variants would fall
	// through to extension-based logic yielding "binary_ignored".
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return "ignore"
	}

	switch base {
	case "dockerfile", "makefile", "jenkinsfile":
		return "code"
	// note: ".env" already handled above via HasPrefix
	case "readme", "license", "changelog":
		return "text"
	case "go.mod", "go.sum", "package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml":
		return "data"
	}

	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".go", ".rs", ".py", ".js", ".jsx", ".ts", ".tsx", ".java", ".c", ".cc", ".cpp", ".h", ".hpp", ".cs", ".rb", ".php", ".swift", ".kt", ".kts", ".scala", ".sh", ".bash", ".zsh", ".sql":
		return "code"
	case ".md", ".markdown", ".mdx", ".rst", ".adoc":
		return "md"
	case ".txt", ".log", ".ini", ".cfg", ".conf":
		return "text"
	case ".csv", ".tsv", ".parquet", ".json", ".jsonl", ".xml", ".yaml", ".yml", ".toml":
		return "data"
	case ".html", ".htm", ".xhtml":
		return "html"
	case ".pdf":
		return "pdf"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".svg":
		return "image"
	case ".mp3", ".wav", ".m4a", ".flac", ".aac", ".ogg", ".opus":
		return "audio"
	case ".zip", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".7z", ".rar":
		return "archive"
	default:
		return "binary_ignored"
	}
}
