package agenttranscript

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Each kind of credential a user pastes is replaced by a placeholder naming it; the prose around it is kept. The credentials are
// built from parts so the repository's own secret scan does not take them for real ones.
//
// spec:endpoint-event-collection/agent-prompt-capture/a-prompt-is-redacted-and-bounded-before-it-leaves
func TestRedact(t *testing.T) {
	t.Parallel()
	pemHead, pemTail := "-----BEGIN "+"RSA PRIVATE KEY-----", "-----END "+"RSA PRIVATE KEY-----"
	for _, tc := range []struct{ name, in, want string }{
		{"an AWS access key", "key AKIA" + "IOSFODNN7EXAMPLE here", "key [REDACTED:aws-access-key] here"},
		{"a GitHub token", "use ghp_" + strings.Repeat("a1", 18), "use [REDACTED:github-token]"},
		{"a fine-grained GitHub token", "github_pat_" + strings.Repeat("Ab3_", 6), "[REDACTED:github-token]"},
		{"a Slack token", "xoxb-" + "1234567890-abcdef", "[REDACTED:slack-token]"},
		{"an Anthropic key", "sk-ant-" + "api03-" + strings.Repeat("x", 30), "[REDACTED:anthropic-key]"},
		{"an OpenAI key", "sk-proj-" + strings.Repeat("Z", 24), "[REDACTED:openai-key]"},
		{"a Google API key", "AIza" + strings.Repeat("B", 35), "[REDACTED:google-api-key]"},
		{"a Stripe key", "sk_" + "live_" + strings.Repeat("9", 24), "[REDACTED:stripe-key]"},
		{"a JWT", "Bearer eyJ" + "hbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NSJ9.c2lnbmF0dXJlLWJ5dGVz", "Bearer [REDACTED:jwt]"},
		{"a private key block", "here:\n" + pemHead + "\nMIIEpQIBAAKCAQEA\n" + pemTail + "\nthanks", "here:\n[REDACTED:private-key]\nthanks"},
		{"a value given a secret's name keeps the name", "set DB_PASSWORD=hunter2hunter2 and go", "set DB_PASSWORD=[REDACTED:secret] and go"},
		{"a quoted JSON secret", `{"api_key": "abcd1234efgh"}`, `{"api_key": "[REDACTED:secret]"}`},
		{"prose about secrets is kept", "rotate the token and update the password policy", "rotate the token and update the password policy"},
		{"a short value is not taken for a secret", "token=abc", "token=abc"},
		{"nothing to redact", "fix the failing auth test", "fix the failing auth test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, n := Redact(tc.in)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, strings.Count(tc.want, "[REDACTED:"), n)
		})
	}
}
