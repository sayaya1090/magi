// Package lang detects the user's written language from their text so the agent and
// the council can reply in it instead of defaulting to English.
package lang

// Detect returns a human-readable label (e.g. "Korean (한국어)") for the dominant
// non-Latin script in text, or "" when it is Latin/undetermined.
func Detect(text string) string {
	var hangul, kana, han, cyrillic, latin int
	for _, r := range text {
		switch {
		case r >= 0xAC00 && r <= 0xD7A3, r >= 0x1100 && r <= 0x11FF:
			hangul++
		case r >= 0x3040 && r <= 0x30FF:
			kana++
		case r >= 0x4E00 && r <= 0x9FFF:
			han++
		case r >= 0x0400 && r <= 0x04FF:
			cyrillic++
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			latin++
		}
	}
	switch {
	case hangul >= 2:
		return "Korean (한국어)"
	case kana >= 2:
		return "Japanese (日本語)"
	case han >= 2 && latin == 0:
		return "Chinese (中文)"
	case cyrillic >= 2:
		return "Russian (русский)"
	}
	return ""
}

// Directive returns a user-facing "reply in <language>" system directive for text, or
// "" if no specific language is detected.
func Directive(text string) string {
	l := Detect(text)
	if l == "" {
		return ""
	}
	return "# Language\nThe user is writing in " + l + ". You MUST write your entire reply to the user in " +
		l + " — not English. Keep only code, identifiers, and file paths as-is."
}
