package main

import (
	"os"
	"strings"
)

// systemLang is "el", "de" or "en": the user's language (LANG/LC_* on
// Linux; on Windows the user's display language, lang_windows.go).
func systemLang() string {
	l, ok := windowsLang()
	if !ok {
		for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
			if s := os.Getenv(v); s != "" {
				l = strings.ToLower(s)
				break
			}
		}
	}
	switch {
	case strings.HasPrefix(l, "el"):
		return "el"
	case strings.HasPrefix(l, "de"):
		return "de"
	}
	return "en"
}

// word picks the tray's text in the user's language.
func word(en, el, de string) string {
	switch systemLang() {
	case "el":
		return el
	case "de":
		return de
	}
	return en
}
