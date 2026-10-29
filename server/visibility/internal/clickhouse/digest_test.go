package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// A name belongs to its registrable domain by the public suffix list, private suffixes included; an IP address, a single label and a
// public suffix stand for themselves.
func TestRegistrableDomain(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, want string }{
		{"api.github.com", "github.com"},
		{"objects.githubusercontent.com", "objects.githubusercontent.com"},
		{"school.k12.ak.us", "school.k12.ak.us"},
		{"93.184.216.34", "93.184.216.34"},
		{"2001:db8::1", "2001:db8::1"},
		{"localhost", "localhost"},
		{"co.uk", "co.uk"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, registrableDomain(c.name))
		})
	}
}

// FoldDomains sums names under one domain, busiest first and then by domain, and keeps the busiest digestDomains of them.
func TestFoldDomains(t *testing.T) {
	t.Parallel()
	got, truncated := foldDomains([]api.DomainActivity{
		{Domain: "api.github.com", Lookups: 1, Connections: 2},
		{Domain: "93.184.216.34", Connections: 1},
		{Domain: "10.0.216.34", Connections: 1},
		{Domain: "gist.github.com", Lookups: 3},
	})
	assert.Equal(t, []api.DomainActivity{
		{Domain: "github.com", Lookups: 4, Connections: 2},
		{Domain: "10.0.216.34", Connections: 1},
		{Domain: "93.184.216.34", Connections: 1},
	}, got)
	assert.False(t, truncated)

	many := make([]api.DomainActivity, digestDomains+1)
	for i := range many {
		many[i] = api.DomainActivity{Domain: string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".com", Lookups: 1}
	}
	got, truncated = foldDomains(many)
	assert.Len(t, got, digestDomains)
	assert.True(t, truncated)
}
