package guardrails

import (
	"regexp"
	"strings"
)

// piiPatterns holds compiled regexes for common PII types.
var piiPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),           // Email
	regexp.MustCompile(`\b\d{3}[-.\s]?\d{2}[-.\s]?\d{4}\b`),                                // SSN
	regexp.MustCompile(`\b(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`),         // Phone
	regexp.MustCompile(`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13})\b`), // Credit card
	regexp.MustCompile(`\b[A-Z]{2}\d{6}[A-Z]?\b`),                                          // Passport-like
}

// MaskPII scans text and replaces any detected PII with [REDACTED].
func MaskPII(text string) string {
	result := text
	for _, pattern := range piiPatterns {
		result = pattern.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}

// MaskJSONBody takes a raw JSON []byte, converts to string, masks PII,
// and returns the masked bytes. Safe to call on any JSON payload.
func MaskJSONBody(body []byte) []byte {
	return []byte(MaskPII(strings.Clone(string(body))))
}
