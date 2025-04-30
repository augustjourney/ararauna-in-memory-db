package config

import (
	"time"
)

type Config struct {
	Server  Server
	Storage Storage
	Logger  Logger
}

type Server struct {
	Port int
}

type Storage struct {
	PartionsNumber int
	GCInterval     time.Duration
	GCBudget       time.Duration
}

type Logger struct {
	Level    string
	FilePath string
}
