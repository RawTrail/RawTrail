package arch_test

// no_agent_attribution_test drives tools/lint-no-agent-attribution.sh, the commit-message gate the commit-msg hook and the required
// "No agent attribution" check both run. A gate that passes everything looks exactly like a gate with nothing to catch, so each
// refusal and the one allowance are pinned here.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoAgentAttribution_RefusesCoAuthorsExceptGitHubsDependabotLine(t *testing.T) {
	t.Parallel()
	script := filepath.Join(repoRootFromTest(t), "tools", "lint-no-agent-attribution.sh")
	// Assembled so this file does not itself carry the strings the gate refuses.
	coAuthor := "Co-" + "Authored-By"

	cases := []struct {
		name    string
		message string
		refused bool
	}{
		{"plain message", "fix: a thing\n\nBecause.\n", false},
		{"prose mentioning the trailer mid-line", "docs: explain why we drop the " + coAuthor + ": trailer\n", false},
		{"human co-author", "fix: a thing\n\n" + coAuthor + ": Jane Doe <jane@example.com>\n", true},
		{"lowercase key", "fix: a thing\n\n" + strings.ToLower(coAuthor) + ": Jane Doe <jane@example.com>\n", true},
		{"agent co-author", "fix: a thing\n\n" + coAuthor + ": Some Agent <noreply@" + "anthropic.com>\n", true},
		{"agent session marker", "fix: a thing\n\n" + "Claude" + "-Session: https://example.com/s\n", true},
		{"generated-by footer", "fix: a thing\n\n" + "Generated with [" + "Claude" + " Code](https://example.com)\n", true},
		{
			"github's dependabot squash trailer", "deps: bump x (#1)\n\n" + strings.ToLower(coAuthor) +
				": dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>\n", false,
		},
		{
			"dependabot name with another address", "deps: bump x (#1)\n\n" + coAuthor +
				": dependabot[bot] <someone@example.com>\n", true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "COMMIT_EDITMSG")
			require.NoError(t, os.WriteFile(file, []byte(tc.message), 0o600))
			out, err := exec.CommandContext(t.Context(), "bash", script, "msg", file).CombinedOutput() //nolint:gosec // fixed script path
			if tc.refused {
				assert.Error(t, err, "the gate must refuse this message; output:\n%s", out)
			} else {
				assert.NoError(t, err, "the gate must accept this message; output:\n%s", out)
			}
		})
	}
}
