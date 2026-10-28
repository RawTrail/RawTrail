package agenttranscript

import "regexp"

// Redacting the secrets a user pastes into a prompt before the prompt leaves the host (issue #1426). Each pattern is the published
// shape of one kind of credential, chosen to fire on the credential and almost never on prose, so a prompt reads as it was typed with
// each credential replaced by a placeholder naming its kind. A credential with no recognizable shape is not caught: the redaction
// narrows what a prompt can carry, it does not make a prompt safe to hold.

// secretPattern is one kind of credential and how it reads.
type secretPattern struct {
	kind string
	re   *regexp.Regexp
	// value is the index of the submatch that holds the secret, for a pattern that matches the secret with the name it is given; 0
	// for a pattern whose whole match is the secret.
	value int
}

var secretPatterns = []secretPattern{
	{kind: "private-key", re: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)},
	{kind: "aws-access-key", re: regexp.MustCompile(`\b(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b`)},
	{kind: "github-token", re: regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b`)},
	{kind: "slack-token", re: regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`)},
	{kind: "anthropic-key", re: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`)},
	{kind: "openai-key", re: regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{20,}`)},
	{kind: "google-api-key", re: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{kind: "stripe-key", re: regexp.MustCompile(`\b[rs]k_live_[0-9A-Za-z]{20,}\b`)},
	{kind: "jwt", re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)},
	// A value given a secret's name: `password=...`, `AWS_SECRET_ACCESS_KEY: ...`, `"api_key": "..."`. The name stays, so the prompt
	// still says what was set.
	{
		kind:  "secret",
		re:    regexp.MustCompile(`(?i)\b[A-Z0-9_]*(?:password|passwd|secret|token|api[_-]?key)["']?\s*[:=]\s*["']?([^\s"',;]{8,})`),
		value: 1,
	},
}

// Redact replaces each credential in text with "[REDACTED:<kind>]" and reports how many it replaced.
func Redact(text string) (string, int) {
	count := 0
	for _, p := range secretPatterns {
		text = p.re.ReplaceAllStringFunc(text, func(match string) string {
			count++
			placeholder := "[REDACTED:" + p.kind + "]"
			if p.value == 0 {
				return placeholder
			}
			sub := p.re.FindStringSubmatchIndex(match)
			return match[:sub[2*p.value]] + placeholder + match[sub[2*p.value+1]:]
		})
	}
	return text, count
}
