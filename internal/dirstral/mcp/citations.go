package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func CitationForSpan(relPath string, span map[string]any) string {
	kind := asString(span["kind"])
	switch kind {
	case "lines":
		start := intVal(span["start_line"])
		end := intVal(span["end_line"])
		if start > 0 && end > 0 {
			return fmt.Sprintf("[%s:L%d-L%d]", relPath, start, end)
		}
	case "page":
		page := intVal(span["page"])
		if page > 0 {
			return fmt.Sprintf("[%s#p=%d]", relPath, page)
		}
	case "time":
		start := intVal(span["start_ms"])
		end := intVal(span["end_ms"])
		if end >= start {
			return fmt.Sprintf("[%s@t=%s-%s]", relPath, formatMs(start), formatMs(end))
		}
	}
	return fmt.Sprintf("[%s]", relPath)
}

func formatMs(ms int) string {
	totalSeconds := ms / 1000
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func intVal(v any) int {
	const maxInt = int(^uint(0) >> 1)

	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case uint:
		if t > uint(maxInt) {
			return maxInt
		}
		return int(t)
	case uint32:
		return int(t)
	case uint64:
		if t > uint64(maxInt) {
			return maxInt
		}
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
		if f, err := t.Float64(); err == nil {
			return int(f)
		}
	case string:
		i, _ := strconv.Atoi(t)
		return i
	}
	return 0
}
