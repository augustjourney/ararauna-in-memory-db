package config

import "time"

type Config struct {
	Storage Storage
}

type Storage struct {
	Shards     int
	GCInterval time.Duration
	GCBudget   time.Duration
}
