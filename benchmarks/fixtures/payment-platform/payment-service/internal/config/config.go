// Package config loads service configuration from the environment.
package config

import (
	"errors"
	"os"
	"strings"
)

// Config is the service configuration.
type Config struct {
	DatabaseDSN  string
	KafkaBrokers []string
	ListenAddr   string
}

// Load reads PAYMENT_DB_DSN, KAFKA_BROKERS and LISTEN_ADDR.
func Load() (Config, error) {
	c := Config{
		DatabaseDSN:  os.Getenv("PAYMENT_DB_DSN"),
		KafkaBrokers: strings.Split(os.Getenv("KAFKA_BROKERS"), ","),
		ListenAddr:   os.Getenv("LISTEN_ADDR"),
	}
	if c.DatabaseDSN == "" {
		return c, errors.New("PAYMENT_DB_DSN is required")
	}
	if c.ListenAddr == "" {
		c.ListenAddr = ":8080"
	}
	return c, nil
}
