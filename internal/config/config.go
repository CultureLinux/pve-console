package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const FileName = ".pve-console"

type Config struct {
	Version   string `json:"version"`
	CreatedAt string `json:"created_at"`
}

func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to detect user home: %w", err)
	}
	return filepath.Join(home, FileName), nil
}

func Ensure(version string) (Config, bool, error) {
	path, err := Path()
	if err != nil {
		return Config{}, false, err
	}

	data, err := os.ReadFile(path)
	if err == nil {
		var cfg Config
		if len(data) > 0 {
			if err := json.Unmarshal(data, &cfg); err != nil {
				return Config{}, false, fmt.Errorf("unable to read %s: %w", path, err)
			}
		}
		return cfg, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, false, fmt.Errorf("unable to read %s: %w", path, err)
	}

	cfg := Config{
		Version:   version,
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return Config{}, false, err
	}
	payload = append(payload, '\n')

	if err := os.WriteFile(path, payload, 0600); err != nil {
		return Config{}, false, fmt.Errorf("unable to create %s: %w", path, err)
	}
	return cfg, true, nil
}
