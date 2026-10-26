package config

import (
	"context"
	"errors"
	"fmt"
	"os"

	configlib "github.com/devctllabs/go-libs/config"
	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/joho/godotenv"
)

// Load resolves generated operator configuration from the process environment and optional .env file.
func Load(ctx context.Context) (generated.Config, error) {
	if err := ctx.Err(); err != nil {
		return generated.Config{}, err
	}
	fileValues, err := godotenv.Read(".env")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return generated.Config{}, fmt.Errorf("load configuration file: %w", err)
	}
	var loaded generated.Config
	loader := configlib.Chain(
		configlib.Defaults(),
		configlib.EnvMap(fileValues),
		configlib.OSEnv(),
	)
	if err := loader.Load(ctx, &loaded); err != nil {
		return generated.Config{}, fmt.Errorf("load configuration: %w", err)
	}
	preserveBootstrapPresence(&loaded.Bootstrap, fileValues)
	if err := loaded.Validate(); err != nil {
		return generated.Config{}, fmt.Errorf("validate configuration: %w", err)
	}
	return loaded, nil
}

// preserveBootstrapPresence restores explicit empty values skipped by the env decoder.
func preserveBootstrapPresence(cfg *generated.BootstrapConfig, fileValues map[string]string) {
	lookup := func(key string) (string, bool) {
		if value, present := os.LookupEnv(key); present {
			return value, true
		}
		value, present := fileValues[key]
		return value, present
	}
	for key, target := range map[string]*string{
		"TOPIC_STREAM_BOOTSTRAP_EMBEDDING_ENDPOINT": &cfg.BootstrapEmbeddingEndpoint,
		"TOPIC_STREAM_BOOTSTRAP_EMBEDDING_MODEL":    &cfg.BootstrapEmbeddingModel,
	} {
		if value, present := lookup(key); present {
			*target = value
		}
	}
	if value, present := lookup("TOPIC_STREAM_BOOTSTRAP_EMBEDDING_DIMENSIONS"); present && value == "" {
		cfg.BootstrapEmbeddingDimensions = 0
	}
}
