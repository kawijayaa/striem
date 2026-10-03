package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/kawijayaa/ksql/kusto"
	"github.com/kawijayaa/striem/internal/api"
)

func nativeKustoFromEnv() (api.NativeKustoConfig, error) {
	endpoint := os.Getenv("STRIEM_KUSTO_ENDPOINT")
	database := os.Getenv("STRIEM_KUSTO_DATABASE")
	tokenFile := os.Getenv("STRIEM_KUSTO_TOKEN_FILE")
	management := os.Getenv("STRIEM_KUSTO_MANAGEMENT")
	if endpoint == "" && database == "" && tokenFile == "" && management == "" {
		return api.NativeKustoConfig{}, nil
	}
	if endpoint == "" || strings.TrimSpace(database) == "" || tokenFile == "" {
		return api.NativeKustoConfig{}, errors.New("native Kusto requires endpoint, database and token file configuration")
	}
	allowManagement := false
	if management != "" {
		var err error
		allowManagement, err = strconv.ParseBool(management)
		if err != nil {
			return api.NativeKustoConfig{}, errors.New("STRIEM_KUSTO_MANAGEMENT must be a boolean")
		}
	}
	provider := func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return readKustoToken(tokenFile)
	}
	client, err := kusto.New(kusto.Config{Endpoint: endpoint, TokenProvider: provider})
	if err != nil {
		return api.NativeKustoConfig{}, err
	}
	if _, err := provider(context.Background()); err != nil {
		return api.NativeKustoConfig{}, err
	}
	return api.NativeKustoConfig{Client: client, Database: database, AllowManagement: allowManagement}, nil
}

func readKustoToken(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("could not open native Kusto token file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("native Kusto token must be in a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return "", errors.New("could not read native Kusto token file within 64 KiB")
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("native Kusto token file is empty")
	}
	for i := range token {
		if token[i] < 33 || token[i] > 126 {
			return "", errors.New("native Kusto token file contains an invalid bearer token")
		}
	}
	return token, nil
}
