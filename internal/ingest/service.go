package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"dir2mcp/internal/appstate"
	"dir2mcp/internal/config"
	"dir2mcp/internal/model"
)

type Service struct {
	cfg           config.Config
	store         model.Store
	indexingState *appstate.IndexingState
	repGen        *RepresentationGenerator
	ocr           model.OCR
}

type documentDeleteMarker interface {
	MarkDocumentDeleted(ctx context.Context, relPath string) error
}

func NewService(cfg config.Config, store model.Store) *Service {
	svc := &Service{
		cfg:   cfg,
		store: store,
	}
	if rs, ok := store.(representationStore); ok {
		svc.repGen = NewRepresentationGenerator(rs)
	}
	return svc
}

func (s *Service) SetIndexingState(state *appstate.IndexingState) {
	s.indexingState = state
}

func (s *Service) SetOCR(ocr model.OCR) {
	s.ocr = ocr
}

func (s *Service) Run(ctx context.Context) error {
	if s.indexingState != nil {
		s.indexingState.SetMode(appstate.ModeIncremental)
		s.indexingState.SetRunning(true)
		defer s.indexingState.SetRunning(false)
	}
	return s.runScan(ctx)
}

func (s *Service) Reindex(ctx context.Context) error {
	if s.indexingState != nil {
		s.indexingState.SetMode(appstate.ModeFull)
		s.indexingState.SetRunning(true)
		defer s.indexingState.SetRunning(false)
	}
	return s.runScan(ctx)
}

func (s *Service) runScan(ctx context.Context) error {
	if s.store == nil {
		return errors.New("ingest store is not configured")
	}

	discovered, err := DiscoverFiles(ctx, s.cfg.RootDir, defaultMaxFileSizeBytes)
	if err != nil {
		return err
	}

	compiledSecrets, err := compileSecretPatterns(s.cfg.SecretPatterns)
	if err != nil {
		return err
	}

	existing, err := s.listActiveDocuments(ctx)
	if err != nil {
		return err
	}

	forceReindex := s.indexingState != nil && s.indexingState.Snapshot().Mode == appstate.ModeFull

	seen := make(map[string]struct{}, len(discovered))
	for _, f := range discovered {
		if err := ctx.Err(); err != nil {
			return err
		}

		s.addScanned(1)
		if matchesAnyPathExclude(f.RelPath, s.cfg.PathExcludes) {
			s.addSkipped(1)
			continue
		}

		if err := s.processDocument(ctx, f, compiledSecrets, forceReindex); err != nil {
			s.addErrors(1)
			// record that we saw the file even if processing failed so
			// markMissingAsDeleted does not treat it as removed
			seen[f.RelPath] = struct{}{}
			continue
		}
		seen[f.RelPath] = struct{}{}
	}

	return s.markMissingAsDeleted(ctx, existing, seen)
}

func (s *Service) processDocument(ctx context.Context, f DiscoveredFile, secretPatterns []*regexp.Regexp, forceReindex bool) error {
	doc, content, buildErr := s.buildDocumentWithContent(f, secretPatterns)
	if buildErr != nil {
		doc = model.Document{
			RelPath:   f.RelPath,
			DocType:   ClassifyDocType(f.RelPath),
			SizeBytes: f.SizeBytes,
			MTimeUnix: f.MTimeUnix,
			Status:    "error",
			Deleted:   false,
		}
		if err := s.store.UpsertDocument(ctx, doc); err != nil {
			return fmt.Errorf("upsert error document: %w", err)
		}
		// s.addErrors(1) is intentionally omitted here; runScan already
		// increments the error counter for any non-nil return value.
		return buildErr
	}

	existingDoc, err := s.store.GetDocumentByPath(ctx, doc.RelPath)
	if err != nil && !isNotFoundError(err) {
		return fmt.Errorf("get existing document: %w", err)
	}

	needsProcessing := needsReprocessing(existingDoc.ContentHash, doc.ContentHash, forceReindex)
	if err := s.store.UpsertDocument(ctx, doc); err != nil {
		return fmt.Errorf("upsert document: %w", err)
	}

	switch doc.Status {
	case "ok":
		s.addIndexed(1)
	case "skipped", "secret_excluded":
		s.addSkipped(1)
	case "error":
		s.addErrors(1)
		return nil
	}

	if !needsProcessing || doc.Status != "ok" {
		return nil
	}

	if err := s.generateRepresentations(ctx, doc, f.AbsPath, content); err != nil {
		return fmt.Errorf("generate representations: %w", err)
	}
	return nil
}

func (s *Service) buildDocumentWithContent(f DiscoveredFile, secretPatterns []*regexp.Regexp) (model.Document, []byte, error) {
	docType := ClassifyDocType(f.RelPath)
	doc := model.Document{
		RelPath:   f.RelPath,
		DocType:   docType,
		SizeBytes: f.SizeBytes,
		MTimeUnix: f.MTimeUnix,
		Status:    "ok",
		Deleted:   false,
	}

	content, err := os.ReadFile(f.AbsPath)
	if err != nil {
		return doc, nil, fmt.Errorf("read %s: %w", f.RelPath, err)
	}
	doc.ContentHash = computeContentHash(content)

	if docType == "archive" || docType == "binary_ignored" {
		doc.Status = "skipped"
		return doc, content, nil
	}

	if hasSecretMatch(contentSample(content), secretPatterns) {
		doc.Status = "secret_excluded"
	}

	return doc, content, nil
}

func contentSample(content []byte) []byte {
	if int64(len(content)) <= secretScanSampleBytes {
		return content
	}
	return content[:secretScanSampleBytes]
}

func (s *Service) listActiveDocuments(ctx context.Context) (map[string]struct{}, error) {
	active := make(map[string]struct{})
	const pageSize = 500

	offset := 0
	for {
		docs, total, err := s.store.ListFiles(ctx, "", "", pageSize, offset)
		if err != nil {
			if errors.Is(err, model.ErrNotImplemented) {
				return active, nil
			}
			return nil, err
		}
		for _, doc := range docs {
			if doc.Deleted {
				continue
			}
			active[doc.RelPath] = struct{}{}
		}

		offset += len(docs)
		if len(docs) == 0 || int64(offset) >= total {
			break
		}
	}
	return active, nil
}

func (s *Service) markMissingAsDeleted(ctx context.Context, existing, seen map[string]struct{}) error {
	deleter, ok := s.store.(documentDeleteMarker)
	if !ok {
		return nil
	}

	paths := make([]string, 0, len(existing))
	for relPath := range existing {
		if _, found := seen[relPath]; found {
			continue
		}
		paths = append(paths, relPath)
	}
	sort.Strings(paths)

	for _, relPath := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := deleter.MarkDocumentDeleted(ctx, relPath); err != nil {
			s.addErrors(1)
			continue
		}
		s.addDeleted(1)
	}
	return nil
}

func (s *Service) addScanned(delta int64) {
	if s.indexingState != nil {
		s.indexingState.AddScanned(delta)
	}
}

func (s *Service) addIndexed(delta int64) {
	if s.indexingState != nil {
		s.indexingState.AddIndexed(delta)
	}
}

func (s *Service) addSkipped(delta int64) {
	if s.indexingState != nil {
		s.indexingState.AddSkipped(delta)
	}
}

func (s *Service) addDeleted(delta int64) {
	if s.indexingState != nil {
		s.indexingState.AddDeleted(delta)
	}
}

func (s *Service) addErrors(delta int64) {
	if s.indexingState != nil {
		s.indexingState.AddErrors(delta)
	}
}

func (s *Service) addRepresentations(delta int64) {
	if s.indexingState != nil {
		s.indexingState.AddRepresentations(delta)
	}
}

func (s *Service) generateRepresentations(ctx context.Context, doc model.Document, absPath string, content []byte) error {
	if s.repGen == nil {
		return nil
	}

	if ShouldGenerateRawText(doc.DocType) {
		if err := s.repGen.GenerateRawText(ctx, doc, absPath); err != nil {
			return err
		}
		s.addRepresentations(1)
		return nil
	}

	if (doc.DocType == "pdf" || doc.DocType == "image") && s.ocr != nil {
		if err := s.generateOCRMarkdownRepresentation(ctx, doc, content); err != nil {
			return err
		}
		s.addRepresentations(1)
	}
	return nil
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// common filesystem sentinel
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	// sqlite/sql driver returns sql.ErrNoRows for missing rows
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	// some store implementations may define their own sentinel error
	// for a missing row/document/representation.  Add a clause here to
	// avoid treating those as fatal.
	if errors.Is(err, model.ErrNotFound) {
		return true
	}
	return false
}

func (s *Service) generateOCRMarkdownRepresentation(ctx context.Context, doc model.Document, content []byte) error {
	if s.repGen == nil || s.ocr == nil {
		return nil
	}

	ocrText, err := s.readOrComputeOCR(ctx, doc, content)
	if err != nil {
		return err
	}

	ocrText = strings.TrimSpace(ocrText)
	if ocrText == "" {
		return nil
	}

	rep := model.Representation{
		DocID:       doc.DocID,
		RepType:     RepTypeOCRMarkdown,
		RepHash:     computeRepHash([]byte(ocrText)),
		CreatedUnix: time.Now().Unix(),
		Deleted:     false,
	}
	repID, err := s.repGen.store.UpsertRepresentation(ctx, rep)
	if err != nil {
		return fmt.Errorf("upsert ocr representation: %w", err)
	}

	segments := chunkOCRByPages(ocrText)
	if len(segments) == 0 {
		return nil
	}
	if err := s.repGen.upsertChunksForRepresentation(ctx, repID, "text", segments); err != nil {
		return fmt.Errorf("persist ocr chunks: %w", err)
	}
	return nil
}

func (s *Service) readOrComputeOCR(ctx context.Context, doc model.Document, content []byte) (string, error) {
	cacheDir := filepath.Join(s.cfg.StateDir, "cache", "ocr")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create ocr cache dir: %w", err)
	}
	cachePath := filepath.Join(cacheDir, computeContentHash(content)+".md")
	if cached, err := os.ReadFile(cachePath); err == nil {
		return string(cached), nil
	}

	ocrText, err := s.ocr.Extract(ctx, doc.RelPath, content)
	if err != nil {
		return "", fmt.Errorf("ocr extract %s: %w", doc.RelPath, err)
	}

	ocrBytes := []byte(strings.ReplaceAll(strings.ReplaceAll(ocrText, "\r\n", "\n"), "\r", "\n"))
	if err := os.WriteFile(cachePath, ocrBytes, 0o644); err != nil {
		return "", fmt.Errorf("write ocr cache: %w", err)
	}
	return string(ocrBytes), nil
}
