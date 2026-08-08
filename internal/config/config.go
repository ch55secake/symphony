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
}

// Load reads the user configuration file and applies environment overrides.
func Load() (Settings, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return Settings{}, fmt.Errorf("get user config directory: %w", err)
	}
	return LoadFile(filepath.Join(dir, "symphony", "config.yaml"))
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
	}, nil
}
