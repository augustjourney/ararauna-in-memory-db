package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  Server  `yaml:"server"`
	Storage Storage `yaml:"storage"`
	Logger  Logger  `yaml:"logger"`
	WAL     WAL     `yaml:"wal"`
	Metrics Metrics `yaml:"metrics"`
}

type Server struct {
	Port            int           `yaml:"port"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

type Storage struct {
	PartitionsNumber int           `yaml:"partitions_number"`
	GCInterval       time.Duration `yaml:"gc_interval"`
	GCBudget         time.Duration `yaml:"gc_budget"`
}

type Logger struct {
	Level    string `yaml:"level"`
	FilePath string `yaml:"file_path"`
}

type WAL struct {
	Enabled     bool   `yaml:"enabled"`
	Dir         string `yaml:"dir"`
	SegmentSize int64  `yaml:"segment_size"`
	SyncPolicy  string `yaml:"sync_policy"`
}

type Metrics struct {
	Enabled      bool              `yaml:"enabled"`
	Provider     string            `yaml:"provider"`
	PushURL      string            `yaml:"push_url"`
	Job          string            `yaml:"job"`
	PushInterval time.Duration     `yaml:"push_interval"`
	Timeout      time.Duration     `yaml:"timeout"`
	ExtraLabels  map[string]string `yaml:"extra_labels"`
}

func Default() *Config {
	return &Config{
		Server: Server{
			Port:            6379,
			ShutdownTimeout: 5 * time.Second,
		},
		Storage: Storage{
			PartitionsNumber: 16,
			GCInterval:       100 * time.Millisecond,
			GCBudget:         50 * time.Millisecond,
		},
		Logger: Logger{
			Level: "info",
		},
		WAL: WAL{
			Enabled:     true,
			Dir:         "./data/wal",
			SegmentSize: 16 * 1024 * 1024,
			SyncPolicy:  "always",
		},
		Metrics: Metrics{
			Enabled:      false,
			Provider:     "victoriametrics",
			PushInterval: 10 * time.Second,
			Job:          "ararauna",
		},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	c := Default()
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	if err := c.validate(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Config) validate() error {
	if c.Storage.PartitionsNumber <= 0 {
		return errors.New("config: storage.partitions_number must be > 0")
	}
	if c.Storage.GCInterval <= 0 {
		return errors.New("config: storage.gc_interval must be > 0")
	}
	if c.Storage.GCBudget <= 0 {
		return errors.New("config: storage.gc_budget must be > 0")
	}
	if c.Server.ShutdownTimeout <= 0 {
		return errors.New("config: server.shutdown_timeout must be > 0")
	}
	if c.WAL.Enabled {
		if c.WAL.Dir == "" {
			return errors.New("config: wal.dir must be set when wal.enabled is true")
		}
		if c.WAL.SegmentSize <= 0 {
			return errors.New("config: wal.segment_size must be > 0")
		}
		switch c.WAL.SyncPolicy {
		case "always", "everysec", "no":
		default:
			return fmt.Errorf("config: wal.sync_policy must be one of always|everysec|no, got %q", c.WAL.SyncPolicy)
		}
	}
	if c.Metrics.Enabled {
		switch c.Metrics.Provider {
		case "victoriametrics", "prometheus":
		default:
			return fmt.Errorf("config: metrics.provider must be one of victoriametrics|prometheus, got %q", c.Metrics.Provider)
		}
		if c.Metrics.PushURL == "" {
			return errors.New("config: metrics.push_url must be set when metrics.enabled is true")
		}
		if c.Metrics.PushInterval <= 0 {
			return errors.New("config: metrics.push_interval must be > 0")
		}
		if c.Metrics.Provider == "prometheus" && c.Metrics.Job == "" {
			return errors.New("config: metrics.job must be set when metrics.provider is prometheus")
		}
	}
	return nil
}
