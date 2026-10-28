package sessioncommands

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// Search keeps the commands a query finds, case-insensitively: in the command the agent ran, or in the path or command line of any
// of its processes. The wrapper shell that ran an agent's command is not searched apart from the command it ran, since its script is
// the same boilerplate for every call. An empty query keeps every command.
func Search(commands []Command, rawQuery string) []Command {
	query := strings.ToLower(strings.TrimSpace(rawQuery))
	if query == "" {
		return commands
	}
	var kept []Command
	for _, c := range commands {
		if strings.Contains(strings.ToLower(c.Command), query) || processMatches(c, query) {
			kept = append(kept, c)
		}
	}
	if kept == nil {
		return []Command{}
	}
	return kept
}

func processMatches(c Command, query string) bool {
	found := false
	for _, root := range c.Roots {
		walk(root, func(p api.ProcessNode) {
			if found || (c.FromAgent && p.ID == c.RootID) {
				return
			}
			line := strings.ToLower(CommandLine(p.Path, args(p)))
			found = strings.Contains(line, query) || strings.Contains(strings.ToLower(p.Path), query)
		})
	}
	return found
}

// cursor is the keyset position of a commands page: the start and root row of the last command of the previous page, which together
// order the commands as Build does.
type cursor struct {
	StartNs int64 `json:"s"`
	RootID  int64 `json:"r"`
}

func encodeCursor(c Command) string {
	raw, err := json.Marshal(cursor{StartNs: c.StartNs, RootID: c.RootID})
	if err != nil {
		panic(err) // unreachable: a struct of two integers always marshals
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(token string) (cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return cursor{}, fmt.Errorf("%w: not base64url: %w", api.ErrInvalidCursor, err)
	}
	var c cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return cursor{}, fmt.Errorf("%w: not a commands cursor: %w", api.ErrInvalidCursor, err)
	}
	// No command starts at zero or has a zero root row, so a cursor missing either names no position.
	if c.StartNs <= 0 || c.RootID <= 0 {
		return cursor{}, fmt.Errorf("%w: incomplete commands cursor", api.ErrInvalidCursor)
	}
	return c, nil
}

// Page is one page of commands, with the cursor for the next (empty on the last) and how many commands the read matched.
type Page struct {
	Commands     []Command
	NextCursor   string
	TotalMatched int
}

// PageOf returns the limit commands after the cursor, in the order Build returns them, or the first limit when the cursor is empty.
// A cursor that does not decode is an api.ErrInvalidCursor.
func PageOf(commands []Command, token string, limit int) (Page, error) {
	limit = max(limit, 1)
	from := 0
	if token != "" {
		c, err := decodeCursor(token)
		if err != nil {
			return Page{}, err
		}
		for from < len(commands) && cmp.Or(cmp.Compare(commands[from].StartNs, c.StartNs), cmp.Compare(commands[from].RootID, c.RootID)) <= 0 {
			from++
		}
	}
	page := Page{Commands: commands[from:min(from+limit, len(commands))], TotalMatched: len(commands)}
	if from+limit < len(commands) {
		page.NextCursor = encodeCursor(page.Commands[len(page.Commands)-1])
	}
	return page, nil
}
