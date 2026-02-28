package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
}

type documentDeleteMarker interface {
	MarkDocumentDeleted(ctx context.Context, relPath string) error
}

func NewService(cfg config.Config, store model.Store) *Service {
	return &Service{
		cfg:   cfg,
		store: store,
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

		doc, buildErr := s.buildDocument(ctx, f, compiledSecrets)
		if buildErr != nil {
			s.addErrors(1)
			doc = model.Document{
				RelPath:   f.RelPath,
				DocType:   ClassifyDocType(f.RelPath),
				SizeBytes: f.SizeBytes,
				MTimeUnix: f.MTimeUnix,
				Status:    "error",
				Deleted:   false,
			}
		}

		if err := s.store.UpsertDocument(ctx, doc); err != nil {
			s.addErrors(1)
			continue
		}
		seen[f.RelPath] = struct{}{}

		switch doc.Status {
		case "ok":
			s.addIndexed(1)
		case "skipped", "secret_excluded":
			s.addSkipped(1)
		case "error":
			s.addErrors(1)
		}
	}

	return s.markMissingAsDeleted(ctx, existing, seen)
}

func (s *Service) buildDocument(_ context.Context, f DiscoveredFile, secretPatterns []*regexp.Regexp) (model.Document, error) {
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
		return doc, fmt.Errorf("read %s: %w", f.RelPath, err)
	}
	sum := sha256.Sum256(content)
	doc.ContentHash = hex.EncodeToString(sum[:])

	if docType == "archive" || docType == "binary_ignored" {
		doc.Status = "skipped"
		return doc, nil
	}

	if hasSecretMatch(contentSample(content), secretPatterns) {
		doc.Status = "secret_excluded"
	}

	return doc, nil
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
