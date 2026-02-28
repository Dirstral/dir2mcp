package ingest

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const archiveMemberMaxBytes = 10 * 1024 * 1024 // 10 MiB

// archiveMember holds extracted content from a single archive entry.
// RelPath is the virtual path "<archiveRelPath>/<memberPath>" used as the
// document's rel_path in the store.
type archiveMember struct {
	RelPath string
	Content []byte
}

// isSafeArchivePath returns true when the member path contains no traversal
// sequences that could escape the extraction root (zip-slip prevention).
func isSafeArchivePath(p string) bool {
	if strings.Contains(p, "..") {
		return false
	}
	// filepath.Clean normalises slashes; reject anything that escapes root.
	cleaned := filepath.ToSlash(filepath.Clean("/" + p))
	return strings.HasPrefix(cleaned, "/") && !strings.HasPrefix(cleaned, "/..")
}

// archiveFormat returns a canonical format string for the archive at relPath,
// or "" if the format is unsupported by the stdlib extractor.
func archiveFormat(relPath string) string {
	name := strings.ToLower(filepath.Base(relPath))
	switch {
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return "tar.gz"
	case strings.HasSuffix(name, ".tar.bz2"):
		return "tar.bz2"
	case strings.HasSuffix(name, ".tar"):
		return "tar"
	case strings.HasSuffix(name, ".zip"):
		return "zip"
	default:
		return ""
	}
}

// extractArchiveMembers dispatches to the correct extractor based on archiveFormat.
// Members that fail path safety checks or exceed archiveMemberMaxBytes are
// silently skipped; corrupted archives return whatever members were read before
// the error.
func extractArchiveMembers(absPath, archiveRelPath string) ([]archiveMember, error) {
	switch archiveFormat(archiveRelPath) {
	case "zip":
		return extractZipMembers(absPath, archiveRelPath)
	case "tar", "tar.gz", "tar.bz2":
		return extractTarMembers(absPath, archiveRelPath)
	default:
		return nil, nil // unsupported format; caller treats as empty
	}
}

func extractZipMembers(absPath, archiveRelPath string) ([]archiveMember, error) {
	r, err := zip.OpenReader(absPath)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	var members []archiveMember
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !isSafeArchivePath(f.Name) {
			continue // zip-slip: skip silently
		}
		if f.UncompressedSize64 > archiveMemberMaxBytes {
			continue // member too large
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(rc, archiveMemberMaxBytes+1))
		_ = rc.Close()
		if readErr != nil || int64(len(content)) > archiveMemberMaxBytes {
			continue
		}
		members = append(members, archiveMember{
			RelPath: archiveRelPath + "/" + f.Name,
			Content: content,
		})
	}
	return members, nil
}

func extractTarMembers(absPath, archiveRelPath string) ([]archiveMember, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("open tar: %w", err)
	}
	defer f.Close()

	var rd io.Reader = f
	switch archiveFormat(archiveRelPath) {
	case "tar.gz":
		gr, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("gzip reader: %w", err)
		}
		defer gr.Close()
		rd = gr
	case "tar.bz2":
		rd = bzip2.NewReader(f)
	}

	tr := tar.NewReader(rd)
	var members []archiveMember
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			break // corrupted entry: return what we have
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		if !isSafeArchivePath(hdr.Name) {
			continue
		}
		if hdr.Size > archiveMemberMaxBytes {
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(tr, archiveMemberMaxBytes+1))
		if readErr != nil || int64(len(content)) > archiveMemberMaxBytes {
			continue
		}
		members = append(members, archiveMember{
			RelPath: archiveRelPath + "/" + hdr.Name,
			Content: content,
		})
	}
	return members, nil
}
