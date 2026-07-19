package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oka/striem/internal/database"
	"github.com/oka/striem/internal/ingest"
)

type Manifest struct {
	Datasets []Dataset `json:"datasets"`
}

type Dataset struct {
	Name            string            `json:"name"`
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

	baseDirectory := filepath.Dir(manifestPath)
	seen := make(map[string]struct{}, len(manifest.Datasets))
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
		names = append(names, configured.Name)
		if strings.TrimSpace(configured.Path) == "" {
			return nil, fmt.Errorf("dataset %q has no path", configured.Name)
		}

		path := configured.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDirectory, path)
		}
		format, err := datasetFormat(path, configured.Format)
		if err != nil {
			return nil, fmt.Errorf("dataset %q: %w", configured.Name, err)
		}
		input, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open dataset %q: %w", configured.Name, err)
		}
		result, importErr := service.Import(ctx, input, strings.EqualFold(filepath.Ext(path), ".gz"), ingest.Mapping{
			Name:            configured.Name,
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
	return loaded, nil
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
