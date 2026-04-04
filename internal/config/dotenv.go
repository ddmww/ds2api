package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadDotEnv loads environment variables from .env in the current working
// directory without overriding variables that are already set.
func LoadDotEnv() error {
	return loadDotEnvFromPath(filepath.Join(BaseDir(), ".env"))
}

func loadDotEnvFromPath(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	for i, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if i == 0 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d invalid env assignment", path, i+1)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("%s:%d empty env key", path, i+1)
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, normalizeDotEnvValue(strings.TrimSpace(value))); err != nil {
			return fmt.Errorf("%s:%d set env %q: %w", path, i+1, key, err)
		}
	}

	return nil
}

func normalizeDotEnvValue(raw string) string {
	if len(raw) < 2 {
		return raw
	}
	first := raw[0]
	last := raw[len(raw)-1]
	if (first != '"' || last != '"') && (first != '\'' || last != '\'') {
		return raw
	}

	raw = raw[1 : len(raw)-1]
	if first == '\'' {
		return raw
	}

	replacer := strings.NewReplacer(
		`\\`, `\`,
		`\n`, "\n",
		`\r`, "\r",
		`\t`, "\t",
		`\"`, `"`,
	)
	return replacer.Replace(raw)
}
