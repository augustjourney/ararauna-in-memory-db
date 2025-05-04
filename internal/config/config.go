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
}

type Server struct {
	Port int `yaml:"port"`
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

func Default() *Config {
	return &Config{
		Server: Server{
			Port: 6379,
		},
		Storage: Storage{
			PartitionsNumber: 16,
			GCInterval:       100 * time.Millisecond,
			GCBudget:         50 * time.Millisecond,
		},
		Logger: Logger{
			Level: "info",
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
	return nil
}
