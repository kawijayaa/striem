package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kawijayaa/striem/internal/database"
	"github.com/kawijayaa/striem/internal/ingest"
)

type Manifest struct {
	Datasets []Dataset `json:"datasets"`
}

type Dataset struct {
	Name            string            `json:"name"`
	Table           string            `json:"table"`
	Path            string            `json:"path"`
	Format          string            `json:"format"`
	Source          string            `json:"source"`
	SourcePath      string            `json:"sourcePath"`
	TimestampPath   string            `json:"timestampPath"`
	TimestampFormat string            `json:"timestampFormat"`
	FieldPaths      map[string]string `json:"fieldPaths"`
}

func Load(ctx context.Context, store *database.Store, manifestPath string) ([]database.Dataset, error) {
	file, err := os.Open(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("open deployment manifest: %w", err)
	}
	defer file.Close()

	var manifest Manifest
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode deployment manifest: %w", err)
	}
	if len(manifest.Datasets) == 0 {
		return nil, fmt.Errorf("deployment manifest contains no datasets")
	}
	baseDirectory, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		return nil, fmt.Errorf("resolve manifest directory: %w", err)
	}
	existingDatasets, err := store.ListDatasets(ctx)
	if err != nil {
		return nil, err
	}
	existingByName := make(map[string]database.Dataset, len(existingDatasets))
	for _, dataset := range existingDatasets {
		existingByName[dataset.Name] = dataset
	}
	indexesDropped := false
	defer func() {
		if indexesDropped {
			_ = store.CreateEventIndexes(context.Background())
		}
	}()
	seen := make(map[string]struct{}, len(manifest.Datasets))
	seenTables := make(map[string]struct{}, len(manifest.Datasets))
	service := ingest.New(store)
	loaded := make([]database.Dataset, 0, len(manifest.Datasets))
	names := make([]string, 0, len(manifest.Datasets))
	for index, configured := range manifest.Datasets {
		if strings.TrimSpace(configured.Name) == "" {
			return nil, fmt.Errorf("dataset %d has no name", index+1)
		}
		if _, duplicate := seen[configured.Name]; duplicate {
			return nil, fmt.Errorf("dataset name %q is configured more than once", configured.Name)
		}
		seen[configured.Name] = struct{}{}
		if strings.TrimSpace(configured.Table) == "" {
			return nil, fmt.Errorf("dataset %q has no table", configured.Name)
		}
		if _, duplicate := seenTables[configured.Table]; duplicate {
			return nil, fmt.Errorf("table %q is configured more than once", configured.Table)
		}
		seenTables[configured.Table] = struct{}{}
		names = append(names, configured.Name)
		if strings.TrimSpace(configured.Path) == "" {
			return nil, fmt.Errorf("dataset %q has no path", configured.Name)
		}

		path := configured.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDirectory, path)
		}
		path = filepath.Clean(path)
		if !strings.HasPrefix(path, baseDirectory+string(filepath.Separator)) && path != baseDirectory {
			return nil, fmt.Errorf("dataset %q path %q escapes the base directory", configured.Name, configured.Path)
		}
		format, err := datasetFormat(path, configured.Format)
		if err != nil {
			return nil, fmt.Errorf("dataset %q: %w", configured.Name, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat dataset %q: %w", configured.Name, err)
		}
		signature, err := datasetSignature(configured, path, info)
		if err != nil {
			return nil, fmt.Errorf("sign dataset %q: %w", configured.Name, err)
		}
		if existing, found := existingByName[configured.Name]; found && existing.Signature == signature {
			loaded = append(loaded, existing)
			continue
		}
		if !indexesDropped {
			if err := store.DropEventIndexes(ctx); err != nil {
				return nil, err
			}
			indexesDropped = true
		}
		input, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open dataset %q: %w", configured.Name, err)
		}
		result, importErr := service.Import(ctx, input, strings.EqualFold(filepath.Ext(path), ".gz"), ingest.Mapping{
			Name:            configured.Name,
			Table:           configured.Table,
			Signature:       signature,
			Format:          format,
			Source:          configured.Source,
			SourcePath:      configured.SourcePath,
			TimestampPath:   configured.TimestampPath,
			TimestampFormat: configured.TimestampFormat,
			FieldPaths:      configured.FieldPaths,
			ReplaceExisting: true,
		})
		closeErr := input.Close()
		if importErr != nil {
			return nil, fmt.Errorf("import dataset %q: %w", configured.Name, importErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close dataset %q: %w", configured.Name, closeErr)
		}
		loaded = append(loaded, result.Dataset)
	}
	if err := store.DeleteDatasetsExcept(ctx, names); err != nil {
		return nil, err
	}
	if indexesDropped {
		if err := store.CreateEventIndexes(ctx); err != nil {
			return nil, err
		}
		indexesDropped = false
	}
	return loaded, nil
}

func datasetSignature(configured Dataset, path string, info os.FileInfo) (string, error) {
	payload := struct {
		Dataset  Dataset `json:"dataset"`
		Path     string  `json:"path"`
		Size     int64   `json:"size"`
		Modified int64   `json:"modified"`
	}{configured, path, info.Size(), info.ModTime().UnixNano()}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signature := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", signature), nil
}

func datasetFormat(path, configured string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(configured))
	if format == "" || format == "auto" {
		basePath := path
		if strings.EqualFold(filepath.Ext(basePath), ".gz") {
			basePath = strings.TrimSuffix(basePath, filepath.Ext(basePath))
		}
		if strings.EqualFold(filepath.Ext(basePath), ".csv") {
			return ingest.FormatCSV, nil
		}
		return ingest.FormatJSON, nil
	}
	if format != ingest.FormatJSON && format != ingest.FormatCSV {
		return "", fmt.Errorf("unsupported format %q; expected auto, json, or csv", configured)
	}
	return format, nil
}
