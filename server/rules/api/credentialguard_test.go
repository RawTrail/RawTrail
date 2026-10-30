package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The default policy is one an operator could have submitted: valid, in detect mode, every built-in marked as a default.
func TestDefaultCredentialGuard_IsValidAndDetects(t *testing.T) {
	t.Parallel()
	d := DefaultCredentialGuard()
	require.NoError(t, ValidateCredentialGuard(d.Mode, d.Credentials))
	assert.Equal(t, CredentialGuardDetect, d.Mode)
	kinds := map[string]bool{}
	for _, c := range d.Credentials {
		assert.True(t, c.Default, c.Path)
		kinds[c.Kind] = true
	}
	assert.Len(t, kinds, len(CredentialKinds), "every kind the guard knows is protected by default")
	// Each call is its own copy, so an edit of one cannot reach the next.
	d.Credentials[0].Owners[0].SigningID = "changed"
	assert.Equal(t, "com.apple.ssh", DefaultCredentialGuard().Credentials[0].Owners[0].SigningID)
}

func TestValidateCredentialGuard(t *testing.T) {
	t.Parallel()
	ok := ProtectedCredential{Kind: "netrc", Path: ".netrc", Match: "literal", Owners: []CredentialOwner{{SigningID: "com.apple.curl"}}}
	with := func(edit func(*ProtectedCredential)) []ProtectedCredential {
		c := ok
		edit(&c)
		return []ProtectedCredential{c}
	}
	tooMany := make([]ProtectedCredential, maxProtectedCredentials+1)
	for i := range tooMany {
		tooMany[i] = ok
		tooMany[i].Path = ".netrc" + strings.Repeat("x", i)
	}
	// Within every count and length bound, but over the encoded budget a 256-host fan-out can carry.
	tooLarge := make([]ProtectedCredential, 32)
	for i := range tooLarge {
		tooLarge[i] = ProtectedCredential{Kind: "netrc", Path: ".netrc" + strings.Repeat("x", i), Match: "literal"}
		for range 16 {
			tooLarge[i].Owners = append(tooLarge[i].Owners, CredentialOwner{SigningID: strings.Repeat("s", 30)})
		}
	}
	cases := []struct {
		name        string
		mode        string
		credentials []ProtectedCredential
		wantErr     string
	}{
		{"protect over one credential", CredentialGuardProtect, []ProtectedCredential{ok}, ""},
		{"off over none", CredentialGuardOff, nil, ""},
		{"an owner by team and signing id", CredentialGuardDetect, with(func(c *ProtectedCredential) {
			c.Owners = []CredentialOwner{{TeamID: "ABCDE12345", SigningID: "gh"}}
		}), ""},
		{"no owners at all", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Owners = nil }), ""},
		{"an unknown mode", "block", []ProtectedCredential{ok}, "mode must be"},
		{"too many credentials", CredentialGuardDetect, tooMany, "at most 64 credentials"},
		{"an unknown kind", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Kind = "cookies" }), "kind must be one of"},
		{"an unknown match", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Match = "glob" }), "match must be"},
		{"an empty path", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Path = "" }), "path must be 1 to"},
		{"a path too long", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Path = strings.Repeat("a", 513) }), "path must be 1 to"},
		{"an absolute path", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Path = "/etc/passwd" }), "below a home"},
		{"a tilde path", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Path = "~/.netrc" }), "below a home"},
		{"a parent segment", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Path = ".aws/../../x" }), "segments"},
		{"an empty segment", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Path = ".aws//credentials" }), "segments"},
		{"an owner with no identity", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Owners = []CredentialOwner{{}} }),
			"a team_id, a signing_id or both"},
		{"a malformed team", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Owners = []CredentialOwner{{TeamID: "abc"}} }),
			"ten capital letters"},
		{"a signing id too long", CredentialGuardDetect, with(func(c *ProtectedCredential) {
			c.Owners = []CredentialOwner{{SigningID: strings.Repeat("s", 256)}}
		}), "longer than 255"},
		{"too many owners", CredentialGuardDetect, with(func(c *ProtectedCredential) {
			c.Owners = make([]CredentialOwner, maxCredentialOwners+1)
			for i := range c.Owners {
				c.Owners[i] = CredentialOwner{SigningID: "x"}
			}
		}), "at most 32 owners"},
		{"a path listed twice", CredentialGuardDetect, []ProtectedCredential{ok, ok}, "listed twice"},
		{"a NUL in the path", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Path = ".aws/credentials\x00suffix" }),
			"control characters"},
		{"a DEL in the path", CredentialGuardDetect, with(func(c *ProtectedCredential) { c.Path = ".net\x7frc" }), "control characters"},
		{"a control character in a signing id", CredentialGuardDetect, with(func(c *ProtectedCredential) {
			c.Owners = []CredentialOwner{{SigningID: "com.apple.curl\n"}}
		}), "control characters"},
		{"a policy over the encoded budget", CredentialGuardDetect, tooLarge, "at most 8192"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCredentialGuard(tc.mode, tc.credentials)
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
