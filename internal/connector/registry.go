package connector

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type descriptor struct {
	dataSourceType string
	newSource      func(Config) (Source, error)
	newSink        func(Config) (Sink, error)
	discover       func(context.Context, Config) (Schema, error)
	validate       func(Config) error
	validateSource func(Config) error
	validateSink   func(Config) error
}

var registry = map[string]descriptor{
	"mysql": {
		dataSourceType: "mysql",
		newSource:      func(cfg Config) (Source, error) { return newSQLSource(cfg, mysqlDialect) },
		newSink:        func(cfg Config) (Sink, error) { return newSQLSink(cfg, mysqlDialect) },
		discover:       func(ctx context.Context, cfg Config) (Schema, error) { return discoverSQL(ctx, cfg, mysqlDialect) },
		validate:       func(cfg Config) error { return validateSQLConnector(cfg, mysqlDialect) },
		validateSource: func(cfg Config) error { return validateSQLSource(cfg, mysqlDialect) },
		validateSink:   func(cfg Config) error { return validateSQLSink(cfg, mysqlDialect) },
	},
	"postgres": {
		dataSourceType: "postgres",
		newSource:      func(cfg Config) (Source, error) { return newSQLSource(cfg, postgresDialect) },
		newSink:        func(cfg Config) (Sink, error) { return newSQLSink(cfg, postgresDialect) },
		discover:       func(ctx context.Context, cfg Config) (Schema, error) { return discoverSQL(ctx, cfg, postgresDialect) },
		validate:       func(cfg Config) error { return validateSQLConnector(cfg, postgresDialect) },
		validateSource: func(cfg Config) error { return validateSQLSource(cfg, postgresDialect) },
		validateSink:   func(cfg Config) error { return validateSQLSink(cfg, postgresDialect) },
	},
	"sqlite": {
		dataSourceType: "sqlite",
		newSource:      func(cfg Config) (Source, error) { return newSQLSource(cfg, sqliteDialect) },
		newSink:        func(cfg Config) (Sink, error) { return newSQLSink(cfg, sqliteDialect) },
		discover:       discoverSQLite,
		validate:       validateSQLite,
		validateSource: func(cfg Config) error { return validateSQLSource(cfg, sqliteDialect) },
		validateSink:   validateSQLiteSink,
	},
	"csv": {
		dataSourceType: "filesystem",
		newSource:      newFileSource,
		newSink:        newFileSink,
		discover:       discoverFile,
		validate:       validateFile,
		validateSource: validateFileSource,
		validateSink:   validateFileSink,
	},
	"txt": {
		dataSourceType: "filesystem",
		newSource:      newFileSource,
		newSink:        newFileSink,
		discover:       discoverFile,
		validate:       validateFile,
		validateSource: validateFileSource,
		validateSink:   validateFileSink,
	},
	"jsonl": {
		dataSourceType: "filesystem",
		newSource:      newFileSource,
		newSink:        newFileSink,
		discover:       discoverFile,
		validate:       validateFile,
		validateSource: validateFileSource,
		validateSink:   validateFileSink,
	},
}

// Supported returns the connector names in stable order.
func Supported() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Validate checks the connector, DataSource type, options and schema.
func Validate(cfg Config) error {
	if err := validateConnectorConfig(cfg); err != nil {
		return err
	}
	return cfg.Schema.Validate()
}

// ValidateSource additionally enforces source-only constraints.
func ValidateSource(cfg Config) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	if extra := registry[strings.ToLower(strings.TrimSpace(cfg.Connector))].validateSource; extra != nil {
		return extra(cfg)
	}
	return nil
}

// ValidateSink additionally enforces sink-only constraints.
func ValidateSink(cfg Config) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	if extra := registry[strings.ToLower(strings.TrimSpace(cfg.Connector))].validateSink; extra != nil {
		return extra(cfg)
	}
	return nil
}

func validateConnectorConfig(cfg Config) error {
	d, ok := registry[strings.ToLower(strings.TrimSpace(cfg.Connector))]
	if !ok {
		return fmt.Errorf("unsupported connector %q", cfg.Connector)
	}
	if cfg.DataSourceType != d.dataSourceType {
		return fmt.Errorf("%s connector requires a %s data source", cfg.Connector, d.dataSourceType)
	}
	return d.validate(cfg)
}

// NewSource constructs a Source after validating the definition.
func NewSource(cfg Config) (Source, error) {
	if err := ValidateSource(cfg); err != nil {
		return nil, err
	}
	return registry[strings.ToLower(strings.TrimSpace(cfg.Connector))].newSource(cfg)
}

// NewSink constructs a Sink after validating the definition.
func NewSink(cfg Config) (Sink, error) {
	if err := ValidateSink(cfg); err != nil {
		return nil, err
	}
	return registry[strings.ToLower(strings.TrimSpace(cfg.Connector))].newSink(cfg)
}

// Discover infers a schema without requiring one in Config.
func Discover(ctx context.Context, cfg Config) (Schema, error) {
	if err := validateConnectorConfig(cfg); err != nil {
		return nil, err
	}
	return registry[strings.ToLower(strings.TrimSpace(cfg.Connector))].discover(ctx, cfg)
}
