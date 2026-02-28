package config

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

func loadDotEnvFiles(paths ...string) error {
	for _, path := range paths {
		if err := loadDotEnvFile(path); err != nil {
			return err
		}
	}
	return nil
}

func loadDotEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}

		existingValue, exists := os.LookupEnv(key)
		if exists && strings.TrimSpace(existingValue) != "" {
			continue
		}
		if err := os.Setenv(key, unquoteEnvValue(value)); err != nil {
			return err
		}
	}

	return scanner.Err()
}

func unquoteEnvValue(v string) string {
	if len(v) >= 2 {
		if strings.HasPrefix(v, "\"") && strings.HasSuffix(v, "\"") {
			return v[1 : len(v)-1]
		}
		if strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") {
			return v[1 : len(v)-1]
		}
	}
	return v
}
