package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kawijayaa/striem/internal/database"
	"github.com/kawijayaa/striem/internal/ingest"
	"gopkg.in/yaml.v3"
)

type Manifest struct {
	ChallengeName      string     `json:"challengeName" yaml:"challengeName"`
	Flag               string     `json:"flag" yaml:"flag"`
	SubmissionCooldown string     `json:"submissionCooldown" yaml:"submissionCooldown"`
	FullTextIndex      bool       `json:"fullTextIndex" yaml:"fullTextIndex"`
	Questions          []Question `json:"questions" yaml:"questions"`
	Datasets           []Dataset  `json:"datasets" yaml:"datasets"`
}

type Question struct {
	ID              string   `json:"id" yaml:"id"`
	Revision        int      `json:"revision" yaml:"revision"`
	Title           string   `json:"title" yaml:"title"`
	Prompt          string   `json:"prompt" yaml:"prompt"`
	AcceptedAnswers []string `json:"acceptedAnswers" yaml:"acceptedAnswers"`
	CaseSensitive   bool     `json:"caseSensitive" yaml:"caseSensitive"`
}

type Dataset struct {
	Name            string   `json:"name" yaml:"name"`
	Table           string   `json:"table" yaml:"table"`
	Path            string   `json:"path" yaml:"path"`
	Format          string   `json:"format" yaml:"format"`
	Source          string   `json:"source" yaml:"source"`
	SourcePath      string   `json:"sourcePath" yaml:"sourcePath"`
	TimestampPath   string   `json:"timestampPath" yaml:"timestampPath"`
	TimestampFormat string   `json:"timestampFormat" yaml:"timestampFormat"`
	IndexedPaths    []string `json:"indexedPaths" yaml:"indexedPaths"`
}

type preparedDataset struct {
	configured Dataset
	path       string
	format     string
	signature  string
}

func Load(ctx context.Context, store *database.Store, manifestPath string) (loaded []database.Dataset, err error) {
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	if len(manifest.Datasets) == 0 {
		return nil, fmt.Errorf("deployment manifest contains no datasets")
	}
	challenge, err := validateChallenge(manifest)
	if err != nil {
		return nil, err
	}
	baseDirectory, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		return nil, fmt.Errorf("resolve manifest directory: %w", err)
	}
	seen := make(map[string]struct{}, len(manifest.Datasets))
	seenTables := make(map[string]struct{}, len(manifest.Datasets))
	indexedPathSet := make(map[string]struct{})
	prepared := make([]preparedDataset, 0, len(manifest.Datasets))
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
		for _, indexedPath := range configured.IndexedPaths {
			if err := database.ValidateIndexedPath(indexedPath); err != nil {
				return nil, fmt.Errorf("dataset %q: %w", configured.Name, err)
			}
			indexedPathSet[indexedPath] = struct{}{}
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
		prepared = append(prepared, preparedDataset{configured: configured, path: path, format: format, signature: signature})
	}
	indexedPaths := make([]string, 0, len(indexedPathSet))
	for path := range indexedPathSet {
		indexedPaths = append(indexedPaths, path)
	}
	sort.Strings(indexedPaths)
	if err := store.ConfigureEventStorage(ctx, indexedPaths, manifest.FullTextIndex); err != nil {
		return nil, fmt.Errorf("configure event storage: %w", err)
	}
	existingDatasets, err := store.ListDatasets(ctx)
	if err != nil {
		return nil, err
	}
	existingByName := make(map[string]database.Dataset, len(existingDatasets))
	for _, dataset := range existingDatasets {
		existingByName[dataset.Name] = dataset
	}
	storageChanged := len(existingDatasets) != len(prepared)
	for _, dataset := range prepared {
		if existing, found := existingByName[dataset.configured.Name]; !found || existing.Signature != dataset.signature {
			storageChanged = true
			break
		}
	}
	derivedDropped := false
	if storageChanged {
		if err := store.DropEventIndexes(ctx); err != nil {
			return nil, err
		}
		derivedDropped = true
		defer func() {
			if !derivedDropped {
				return
			}
			restoreErr := store.CreateEventIndexes(context.Background())
			if fullTextErr := store.SyncFullTextIndex(context.Background(), true); fullTextErr != nil {
				restoreErr = errors.Join(restoreErr, fullTextErr)
			}
			if restoreErr != nil {
				err = errors.Join(err, restoreErr)
			}
		}()
		if err := store.DropFullTextIndex(ctx); err != nil {
			return nil, err
		}
	}

	service := ingest.New(store)
	loaded = make([]database.Dataset, 0, len(prepared))
	for _, dataset := range prepared {
		configured := dataset.configured
		if existing, found := existingByName[configured.Name]; found && existing.Signature == dataset.signature {
			loaded = append(loaded, existing)
			continue
		}
		input, err := os.Open(dataset.path)
		if err != nil {
			return nil, fmt.Errorf("open dataset %q: %w", configured.Name, err)
		}
		result, importErr := service.Import(ctx, input, strings.EqualFold(filepath.Ext(dataset.path), ".gz"), ingest.Mapping{
			Name:            configured.Name,
			Table:           configured.Table,
			Signature:       dataset.signature,
			Format:          dataset.format,
			Source:          configured.Source,
			SourcePath:      configured.SourcePath,
			TimestampPath:   configured.TimestampPath,
			TimestampFormat: configured.TimestampFormat,
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
	if derivedDropped {
		if err := store.CreateEventIndexes(ctx); err != nil {
			return nil, err
		}
		if err := store.SyncFullTextIndex(ctx, true); err != nil {
			return nil, err
		}
		derivedDropped = false
	} else {
		if err := store.CreateEventIndexes(ctx); err != nil {
			return nil, err
		}
		if err := store.SyncFullTextIndex(ctx, false); err != nil {
			return nil, err
		}
	}
	if err := store.SetChallengeName(ctx, manifest.ChallengeName); err != nil {
		return nil, err
	}
	if err := store.ConfigureChallenge(ctx, challenge); err != nil {
		return nil, err
	}
	return loaded, nil
}

// ReadChallengeName reads the display name without waiting for dataset ingestion.
func ReadChallengeName(manifestPath string) (string, error) {
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return "", err
	}
	return manifest.ChallengeName, nil
}

func readManifest(manifestPath string) (Manifest, error) {
	file, err := os.Open(manifestPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("open deployment manifest: %w", err)
	}
	defer file.Close()

	var manifest Manifest
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode deployment manifest: %w", err)
	}
	manifest.ChallengeName = strings.TrimSpace(manifest.ChallengeName)
	if len(manifest.ChallengeName) > 120 {
		return Manifest{}, fmt.Errorf("challengeName cannot exceed 120 characters")
	}
	return manifest, nil
}

var questionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func validateChallenge(manifest Manifest) (database.ChallengeDefinition, error) {
	cooldown := 3 * time.Second
	if strings.TrimSpace(manifest.SubmissionCooldown) != "" {
		parsed, err := time.ParseDuration(manifest.SubmissionCooldown)
		if err != nil {
			return database.ChallengeDefinition{}, fmt.Errorf("submissionCooldown: %w", err)
		}
		if parsed < 0 || parsed > time.Minute {
			return database.ChallengeDefinition{}, fmt.Errorf("submissionCooldown must be between 0s and 1m")
		}
		cooldown = parsed
	}
	flag := strings.TrimSpace(manifest.Flag)
	if len(manifest.Questions) > 0 && flag == "" {
		return database.ChallengeDefinition{}, fmt.Errorf("flag is required when questions are configured")
	}
	if len(flag) > 512 {
		return database.ChallengeDefinition{}, fmt.Errorf("flag cannot exceed 512 characters")
	}
	if len(manifest.Questions) > 100 {
		return database.ChallengeDefinition{}, fmt.Errorf("questions cannot contain more than 100 entries")
	}
	challenge := database.ChallengeDefinition{Flag: flag, Cooldown: cooldown, Questions: make([]database.QuestionDefinition, 0, len(manifest.Questions))}
	seen := make(map[string]struct{}, len(manifest.Questions))
	for index, configured := range manifest.Questions {
		configured.ID = strings.TrimSpace(configured.ID)
		if !questionIDPattern.MatchString(configured.ID) {
			return database.ChallengeDefinition{}, fmt.Errorf("question %d id must match %s", index+1, questionIDPattern.String())
		}
		if _, duplicate := seen[configured.ID]; duplicate {
			return database.ChallengeDefinition{}, fmt.Errorf("question id %q is configured more than once", configured.ID)
		}
		seen[configured.ID] = struct{}{}
		if configured.Revision == 0 {
			configured.Revision = 1
		}
		if configured.Revision < 1 {
			return database.ChallengeDefinition{}, fmt.Errorf("question %q revision must be positive", configured.ID)
		}
		configured.Title = strings.TrimSpace(configured.Title)
		if configured.Title == "" || len(configured.Title) > 120 {
			return database.ChallengeDefinition{}, fmt.Errorf("question %q title must contain 1 to 120 characters", configured.ID)
		}
		configured.Prompt = strings.TrimSpace(configured.Prompt)
		if configured.Prompt == "" || len(configured.Prompt) > 8192 {
			return database.ChallengeDefinition{}, fmt.Errorf("question %q prompt must contain 1 to 8192 characters", configured.ID)
		}
		if len(configured.AcceptedAnswers) == 0 || len(configured.AcceptedAnswers) > 20 {
			return database.ChallengeDefinition{}, fmt.Errorf("question %q must contain 1 to 20 acceptedAnswers", configured.ID)
		}
		answers := make([]string, 0, len(configured.AcceptedAnswers))
		answerSet := make(map[string]struct{}, len(configured.AcceptedAnswers))
		for _, answer := range configured.AcceptedAnswers {
			answer = strings.TrimSpace(answer)
			if answer == "" || len(answer) > 512 {
				return database.ChallengeDefinition{}, fmt.Errorf("question %q acceptedAnswers must contain 1 to 512 characters", configured.ID)
			}
			key := answer
			if !configured.CaseSensitive {
				key = strings.ToLower(key)
			}
			if _, duplicate := answerSet[key]; duplicate {
				return database.ChallengeDefinition{}, fmt.Errorf("question %q contains duplicate acceptedAnswers", configured.ID)
			}
			answerSet[key] = struct{}{}
			answers = append(answers, answer)
		}
		challenge.Questions = append(challenge.Questions, database.QuestionDefinition{
			ID: configured.ID, Revision: configured.Revision, Title: configured.Title, Prompt: configured.Prompt,
			AcceptedAnswers: answers, CaseSensitive: configured.CaseSensitive,
		})
	}
	return challenge, nil
}

const schemaVersion = 4

func datasetSignature(configured Dataset, path string, info os.FileInfo) (string, error) {
	payload := struct {
		SchemaVersion int     `json:"schemaVersion"`
		Dataset       Dataset `json:"dataset"`
		Path          string  `json:"path"`
		Size          int64   `json:"size"`
		Modified      int64   `json:"modified"`
	}{schemaVersion, configured, path, info.Size(), info.ModTime().UnixNano()}
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
		if strings.EqualFold(filepath.Ext(basePath), ".evtx") {
			return ingest.FormatEVTX, nil
		}
		return ingest.FormatJSON, nil
	}
	if format != ingest.FormatJSON && format != ingest.FormatCSV && format != ingest.FormatEVTX {
		return "", fmt.Errorf("unsupported format %q; expected auto, json, csv, or evtx", configured)
	}
	return format, nil
}
