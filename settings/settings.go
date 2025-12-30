package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AppSettings contains user preferences for file transfers
type AppSettings struct {
	AutoHash      bool `json:"autoHash"`
	Compress      bool `json:"compress"`
	CacheManifest bool `json:"cacheManifest"`
}

// GetSettingsPath returns the path to the settings file
func GetSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".2c1f-settings.json")
}

// LoadSettings loads settings from the JSON file or returns safe defaults
func LoadSettings() AppSettings {
	path := GetSettingsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		// Return safe defaults if file doesn't exist or can't be read
		return AppSettings{
			AutoHash:      true,
			Compress:      false,
			CacheManifest: true,
		}
	}

	var settings AppSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		// Return safe defaults if JSON is corrupted
		return AppSettings{
			AutoHash:      true,
			Compress:      false,
			CacheManifest: true,
		}
	}

	return settings
}
