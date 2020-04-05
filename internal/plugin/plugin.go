// Package plugin for caching directories using given backends
package plugin

import (
	"errors"
	"fmt"
	"os"

	"github.com/meltwater/drone-cache/archive"
	"github.com/meltwater/drone-cache/cache"
	"github.com/meltwater/drone-cache/internal/metadata"
	"github.com/meltwater/drone-cache/key"
	keygen "github.com/meltwater/drone-cache/key/generator"
	"github.com/meltwater/drone-cache/storage"
	"github.com/meltwater/drone-cache/storage/backend"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

// Error recognized error from plugin.
type Error string

// Error TODO
func (e Error) Error() string { return string(e) }

// Unwrap TODO
func (e Error) Unwrap() error { return e }

// Plugin stores metadata about current plugin.
type Plugin struct {
	logger log.Logger

	Metadata metadata.Metadata
	Config   Config
}

// New TODO
func New(logger log.Logger) *Plugin {
	return &Plugin{logger: logger}
}

// Exec entry point of Plugin, where the magic happens.
func (p *Plugin) Exec() error {
	cfg := p.Config

	// 1. Check parameters
	if cfg.Debug {
		level.Debug(p.logger).Log("msg", "DEBUG MODE enabled!")

		for _, pair := range os.Environ() {
			level.Debug(p.logger).Log("var", pair)
		}

		level.Debug(p.logger).Log("msg", "plugin initialized wth config", "config", fmt.Sprintf("%#v", p.Config))
		level.Debug(p.logger).Log("msg", "plugin initialized with metadata", "metadata", fmt.Sprintf("%#v", p.Metadata))
	}

	// FLUSH

	if cfg.Rebuild && cfg.Restore {
		return errors.New("rebuild and restore are mutually exclusive, please set only one of them")
	}

	workspace, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory %w", err)
	}

	var options []cache.Option
	options = append(options, cache.WithNamespace(p.Metadata.Repo.Name))

	var generator key.Generator
	if cfg.CacheKeyTemplate != "" {
		generator = keygen.NewMetadata(p.logger, cfg.CacheKeyTemplate, p.Metadata)
		if err := generator.Check(); err != nil {
			return fmt.Errorf("parse failed, falling back to default %w", err)
		}

		options = append(options, cache.WithFallbackGenerator(keygen.NewHash(p.Metadata.Commit.Branch)))
	} else {
		generator = keygen.NewHash(p.Metadata.Commit.Branch)
		options = append(options, cache.WithFallbackGenerator(keygen.NewStatic(p.Metadata.Commit.Branch)))
	}

	// 2. Initialize storage backend.
	b, err := backend.FromConfig(p.logger, cfg.Backend, backend.Config{
		Debug:      cfg.Debug,
		Azure:      cfg.Azure,
		FileSystem: cfg.FileSystem,
		GCS:        cfg.GCS,
		S3:         cfg.S3,
		SFTP:       cfg.SFTP,
	})
	if err != nil {
		return fmt.Errorf("initialize backend <%s> %w", cfg.Backend, err)
	}

	// 3. Initialize cache.
	c := cache.New(p.logger,
		storage.New(p.logger, b, cfg.StorageOperationTimeout),
		archive.FromFormat(p.logger, workspace, cfg.ArchiveFormat,
			archive.WithSkipSymlinks(cfg.SkipSymlinks),
			archive.WithCompressionLevel(cfg.CompressionLevel),
		),
		generator,
		options...,
	)

	// 4. Select mode
	if cfg.Rebuild {
		if err := c.Rebuild(p.Config.Mount); err != nil {
			level.Debug(p.logger).Log("err", fmt.Sprintf("%+v\n", err))
			return Error(fmt.Sprintf("[IMPORTANT] build cache, %+v\n", err))
		}
	}

	if cfg.Restore {
		if err := c.Restore(p.Config.Mount); err != nil {
			level.Debug(p.logger).Log("err", fmt.Sprintf("%+v\n", err))
			return Error(fmt.Sprintf("[IMPORTANT] restore cache, %+v\n", err))
		}
	}

	// FLUSH

	return nil
}
