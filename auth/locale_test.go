package auth

import (
	"strings"
	"testing"
)

func TestAccountLocaleCoverage(t *testing.T) {
	en, zh := labels("en"), labels("zh-CN")
	if len(en) < 30 || len(en) != len(zh) {
		t.Fatal("account locale keys are incomplete")
	}
	for key, english := range en {
		if english == "" || zh[key] == "" || strings.Count(english, "%@") != strings.Count(zh[key], "%@") {
			t.Fatalf("missing translation or changed format arguments: %s", key)
		}
	}
	for _, locale := range []string{"zh", "zh-CN", "zh-Hans", "zh-SG"} {
		if labels(locale)["connect"] != "连接" {
			t.Fatal("Chinese locale was not selected")
		}
	}
	if labels("en-GB")["connect"] != "Connect" || labels("fr-FR")["connect"] != "Connect" {
		t.Fatal("English fallback missing")
	}
}

func TestFallbackDialogsUseClientAndLocale(t *testing.T) {
	for _, language := range []string{"en", "zh-CN"} {
		for _, code := range []bool{false, true} {
			prompt := SystemPrompt{Language: language, AppName: "MyGo Music's player"}
			script := prompt.windowsForm(code)
			if strings.Contains(script, "@@") || !strings.Contains(script, "MyGo Music''s player") || !strings.Contains(script, labels(language)["cancel"]) {
				t.Fatal("fallback dialog lost locale or escaped client attribution")
			}
		}
	}
}
