package agenttranscript

import (
	"regexp"
	"strings"
)

// Redacting the secrets a user pastes into a prompt before the prompt leaves the host (issue #1426). Each pattern is the published
// shape of one kind of credential, chosen to fire on the credential and almost never on prose, so a prompt reads as it was typed with
// each credential replaced by a placeholder naming its kind. A credential with no recognizable shape is not caught: the redaction
// narrows what a prompt can carry, it does not make a prompt safe to hold.

// secretPattern is one kind of credential and how it reads.
type secretPattern struct {
	kind string
	re   *regexp.Regexp
	// value is the index of the first submatch that can hold the secret, for a pattern that matches the secret with the name it is
	// given (later submatches are alternative forms of it); 0 for a pattern whose whole match is the secret.
	value int
}

var secretPatterns = []secretPattern{
	{kind: "private-key", re: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`)},
	{kind: "aws-access-key", re: regexp.MustCompile(`\b(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}\b`)},
	{kind: "github-token", re: regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b`)},
	{kind: "slack-token", re: regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`)},
	{kind: "anthropic-key", re: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`)},
	{kind: "openai-key", re: regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{20,}`)},
	// No trailing \b: a key may end in "-", after which a word boundary never falls.
	{kind: "google-api-key", re: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`)},
	{kind: "stripe-key", re: regexp.MustCompile(`\b[rs]k_live_[0-9A-Za-z]{20,}\b`)},
	{kind: "jwt", re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)},
	// A value given a secret's name: `password=...`, `AWS_SECRET_ACCESS_KEY: ...`, `"api_key": "..."`. The name stays, so the prompt
	// still says what was set. A quoted value is the secret through its closing quote, so a passphrase with spaces is redacted whole;
	// an unquoted one runs to the first space or delimiter.
	{
		kind: "secret",
		re: regexp.MustCompile(`(?i)\b[A-Z0-9_]*(?:password|passwd|secret|token|api[_-]?key)[A-Z0-9_]*["']?\s*[:=]\s*` +
			`(?:"([^"\n]+)"|'([^'\n]+)'|([^\s"',;]{8,}))`),
		value: 1,
	},
}

// Redact replaces each credential in text with "[REDACTED:<kind>]" and reports how many it replaced.
func Redact(text string) (string, int) {
	count := 0
	for _, p := range secretPatterns {
		placeholder := "[REDACTED:" + p.kind + "]"
		text = p.re.ReplaceAllStringFunc(text, func(match string) string {
			if p.value == 0 {
				count++
				return placeholder
			}
			start, end := valueSpan(p, match)
			// An earlier pattern may already have replaced this credential with its own kind, which is the truer name for it.
			if start < 0 || strings.HasPrefix(match[start:end], "[REDACTED:") {
				return match
			}
			count++
			return match[:start] + placeholder + match[end:]
		})
	}
	return text, count
}

// valueSpan is where a named pattern's secret sits in its match: the first of its value groups, from p.value on, that took part (a
// quoted value and a bare one are alternatives). -1 when none did.
func valueSpan(p secretPattern, match string) (int, int) {
	sub := p.re.FindStringSubmatchIndex(match)
	for g := p.value; 2*g+1 < len(sub); g++ {
		if sub[2*g] >= 0 {
			return sub[2*g], sub[2*g+1]
		}
	}
	return -1, -1
}
