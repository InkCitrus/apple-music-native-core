package auth

import (
	"embed"
	"encoding/json"
	"os"
	"strings"
)

//go:embed locales/*.json
var localeFiles embed.FS

func localeName(language string) string {
	if language == "" {
		language = os.Getenv("LC_ALL")
		if language == "" {
			language = os.Getenv("LANG")
		}
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(language)), "zh") {
		return "zh-Hans"
	}
	return "en"
}
func labels(language string) map[string]string {
	data, _ := localeFiles.ReadFile("locales/" + localeName(language) + ".json")
	var result map[string]string
	json.Unmarshal(data, &result)
	return result
}
func (s SystemPrompt) clientName() string {
	name := strings.TrimSpace(s.AppName)
	if name == "" {
		return "Apple Music Native Core"
	}
	return name
}
