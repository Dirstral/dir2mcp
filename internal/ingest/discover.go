package ingest

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultMaxFileSizeBytes int64 = 10 * 1024 * 1024

var defaultExcludedDirs = map[string]struct{}{
	".git":         {},
	".dir2mcp":     {},
	"node_modules": {},
	"vendor":       {},
	"__pycache__":  {},
}

// DiscoveredFile holds metadata collected during file system discovery.
type DiscoveredFile struct {
	AbsPath   string
	RelPath   string
	SizeBytes int64
	MTimeUnix int64
	Mode      os.FileMode
}

// DiscoverFiles walks rootDir and returns regular files that pass default
// discovery policies (skip symlinks, known heavy dirs, and over-limit files).
func DiscoverFiles(ctx context.Context, rootDir string, maxSizeBytes int64) ([]DiscoveredFile, error) {
	if maxSizeBytes <= 0 {
		maxSizeBytes = defaultMaxFileSizeBytes
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}

	files := make([]DiscoveredFile, 0, 256)
	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		// Never follow symlinks.
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			if path == absRoot {
				return nil
			}
			if shouldSkipDirectory(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if !d.Type().IsRegular() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxSizeBytes {
			return nil
		}

		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" {
			return nil
		}

		files = append(files, DiscoveredFile{
			AbsPath:   path,
			RelPath:   rel,
			SizeBytes: info.Size(),
			MTimeUnix: info.ModTime().Unix(),
			Mode:      info.Mode(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })
	return files, nil
}

func shouldSkipDirectory(name string) bool {
	_, ok := defaultExcludedDirs[strings.TrimSpace(name)]
	return ok
}
