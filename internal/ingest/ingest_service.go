package ingest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"

	"dir2mcp/internal/appstate"
	"dir2mcp/internal/config"
	"dir2mcp/internal/model"
)

type Service struct {
	cfg           config.Config
	store         model.Store
	indexingState *appstate.IndexingState
	repGen        *RepresentationGenerator
}

type documentDeleteMarker interface {
	MarkDocumentDeleted(ctx context.Context, relPath string) error
}

func NewService(cfg config.Config, store model.Store) *Service {
	return &Service{
		cfg:    cfg,
		store:  store,
		repGen: NewRepresentationGenerator(store),
	}
}

func (s *Service) SetIndexingState(state *appstate.IndexingState) {
	s.indexingState = state
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

	// Determine if we're in full reindex mode
	forceReindex := s.indexingState != nil && s.indexingState.GetMode() == appstate.ModeFull

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

		// Process document with hash-based change detection
		if err := s.processDocument(ctx, f, compiledSecrets, forceReindex); err != nil {
			s.addErrors(1)
			// Continue processing other documents
			continue
		}
		seen[f.RelPath] = struct{}{}
	}

	return s.markMissingAsDeleted(ctx, existing, seen)
}

// processDocument handles the complete document processing pipeline:
// 1. Build document metadata and compute content hash
// 2. Check if document needs reprocessing (hash-based change detection)
// 3. Upsert document to store
// 4. Generate representations if needed
func (s *Service) processDocument(ctx context.Context, f DiscoveredFile, secretPatterns []*regexp.Regexp, forceReindex bool) error {
	// Build document with hash
	doc, content, buildErr := s.buildDocumentWithContent(ctx, f, secretPatterns)
	if buildErr != nil {
		// Create error document
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
		s.addErrors(1)
		return buildErr
	}

	// Check if document already exists and compare hashes (incremental indexing)
	existingDoc, err := s.store.GetDocumentByPath(ctx, doc.RelPath)
	if err != nil && !isNotFoundError(err) {
		return fmt.Errorf("get existing document: %w", err)
	}

	// Determine if we need to reprocess this document
	needsProcessing := needsReprocessing(existingDoc.ContentHash, doc.ContentHash, forceReindex)
	
	// Always upsert the document to update mtime and other metadata
	if err := s.store.UpsertDocument(ctx, doc); err != nil {
		return fmt.Errorf("upsert document: %w", err)
	}

	// Update counters based on document status
	switch doc.Status {
	case "ok":
		s.addIndexed(1)
	case "skipped", "secret_excluded":
		s.addSkipped(1)
	case "error":
		s.addErrors(1)
		return nil
	}

	// If document doesn't need reprocessing and we're in incremental mode, skip representation generation
	if !needsProcessing {
		return nil
	}

	// Generate representations for documents with "ok" status
	if doc.Status == "ok" {
		if err := s.generateRepresentations(ctx, doc, f.AbsPath, content); err != nil {
			// Log error but don't fail the entire pipeline
			return fmt.Errorf("generate representations: %w", err)
		}
	}

	return nil
}

// buildDocumentWithContent builds a document and returns both the document and its content.
// The content is returned separately to avoid reading the file twice for representation generation.
func (s *Service) buildDocumentWithContent(ctx context.Context, f DiscoveredFile, secretPatterns []*regexp.Regexp) (model.Document, []byte, error) {
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
	
	// Compute content hash for incremental indexing
	doc.ContentHash = computeContentHash(content)

	// Skip binary and archive files
	if docType == "archive" || docType == "binary_ignored" {
		doc.Status = "skipped"
		return doc, content, nil
	}

	// Check for secrets
	if hasSecretMatch(contentSample(content), secretPatterns) {
		doc.Status = "secret_excluded"
	}

	return doc, content, nil
}

// generateRepresentations creates appropriate representations for a document
// based on its doc_type. Currently implements raw_text generation.
func (s *Service) generateRepresentations(ctx context.Context, doc model.Document, absPath string, content []byte) error {
	// Generate raw_text representation for text-based documents
	if ShouldGenerateRawText(doc.DocType) {
		if err := s.repGen.GenerateRawText(ctx, doc, absPath); err != nil {
			return fmt.Errorf("generate raw_text: %w", err)
		}
		s.addRepresentations(1)
	}

	// Future: Add OCR for PDFs/images, transcription for audio, etc.
	// These will be implemented in subsequent parts of issue #6

	return nil
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

// isNotFoundError checks if an error indicates a document was not found.
// This is a helper for hash-based change detection.
func isNotFoundError(err error) bool {
	// This should check for the specific NotFound error from the store
	// Implementation depends on the actual error types returned by model.Store
	return err != nil && errors.Is(err, model.ErrNotFound)
}
