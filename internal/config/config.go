// Package config loads Symphony's process configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Settings contains configuration that may be supplied by a file or environment.
type Settings struct {
	KurrentDBURL    string
	Provider        string
	Transport       string
	Model           string
	Workspace       string
	OpenAIAPIKey    string
	AnthropicAPIKey string
	OpenCodeAPIKey  string
	Theme           string
}

// Load reads the user configuration file and applies environment overrides.
func Load() (Settings, error) {
	path, err := configPath()
	if err != nil {
		return Settings{}, err
	}
	return LoadFile(path)
}

// Path returns the user configuration file path.
func Path() string {
	path, _ := configPath()
	return path
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("get user config directory: %w", err)
	}
	return filepath.Join(dir, "symphony", "config.yaml"), nil
}

// LoadFile reads configuration from path and applies environment overrides. An absent file
// is valid so Symphony can continue to be configured entirely through flags and environment.
func LoadFile(path string) (Settings, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetDefault("transport", "responses")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.Is(err, os.ErrNotExist) && !errors.As(err, &notFound) {
			return Settings{}, fmt.Errorf("read configuration: %w", err)
		}
	}
	return Settings{
		KurrentDBURL:    v.GetString("kurrentdb_url"),
		Provider:        v.GetString("provider"),
		Transport:       v.GetString("transport"),
		Model:           v.GetString("model"),
		Workspace:       v.GetString("workspace"),
		OpenAIAPIKey:    v.GetString("openai_api_key"),
		AnthropicAPIKey: v.GetString("anthropic_api_key"),
		OpenCodeAPIKey:  v.GetString("opencode_api_key"),
		Theme:           v.GetString("theme"),
	}, nil
}

// SaveTheme persists the selected built-in terminal theme.
func SaveTheme(theme string) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	return saveTheme(path, theme)
}

func saveTheme(path, theme string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close config file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("restrict config permissions: %w", err)
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.Is(err, os.ErrNotExist) && !errors.As(err, &notFound) {
			return fmt.Errorf("read configuration: %w", err)
		}
	}
	v.Set("theme", theme)
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("write configuration: %w", err)
	}
	return nil
}

// SaveConnection persists a provider, its API key, and the selected model.
func SaveConnection(provider, apiKey, model string) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	return saveConnection(path, provider, apiKey, model)
}

func saveConnection(path, provider, apiKey, model string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	keyName, err := connectionKeyName(provider)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close config file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("restrict config permissions: %w", err)
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.Is(err, os.ErrNotExist) && !errors.As(err, &notFound) {
			return fmt.Errorf("read configuration: %w", err)
		}
	}
	v.Set("provider", provider)
	v.Set("model", model)
	v.Set(keyName, apiKey)
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("write configuration: %w", err)
	}
	return nil
}

func connectionKeyName(provider string) (string, error) {
	switch provider {
	case "openai":
		return "openai_api_key", nil
	case "anthropic":
		return "anthropic_api_key", nil
	case "opencode", "opencode-go":
		return "opencode_api_key", nil
	default:
		return "", fmt.Errorf("unknown provider %q", provider)
	}
}
