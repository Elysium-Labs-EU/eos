package config

import (
	"fmt"
	"os"
	"path/filepath"
)

func GetBaseDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine base directory: %w", err)
	}
	return filepath.Join(homeDir, ".eos"), nil
}
