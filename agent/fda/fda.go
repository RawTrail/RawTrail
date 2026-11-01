// Package fda reads whether the agent and its security extension have Full Disk Access (issue #1436), so a missing grant is reported as
// itself rather than as an extension that never connected. macOS grants it per program, and both need it: the extension cannot get an
// Endpoint Security client without it, and macOS refuses even root a read of a privacy-protected file without it.
package fda

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
)

const (
	// TCCDatabase is the system privacy database. macOS lets a process read it only with Full Disk Access, root or not, which makes
	// opening it the standard test of the grant.
	TCCDatabase = "/Library/Application Support/com.apple.TCC/TCC.db"
	// ExtensionStatus is where the security extension records why macOS refused it an Endpoint Security client (ESClientStatus.swift),
	// removed once a client is granted.
	ExtensionStatus = "/var/db/io.rawtrail/es-client-status.json"
	// RefusedNotPermitted is the refusal that means the extension lacks Full Disk Access.
	RefusedNotPermitted = "not_permitted"
)

// Grant is what a read of the agent's own Full Disk Access found.
type Grant int

const (
	// GrantUnknown is a read that failed for another reason than permission, which says nothing about the grant.
	GrantUnknown Grant = iota
	GrantGiven
	GrantMissing
)

// Probe reads the grant and the refusal from their paths, which the tests point elsewhere.
type Probe struct {
	TCCPath    string
	StatusPath string
}

// NewProbe reads the real paths.
func NewProbe() Probe {
	return Probe{TCCPath: TCCDatabase, StatusPath: ExtensionStatus}
}

// AgentGrant opens the privacy database for reading: macOS allows it with Full Disk Access and answers "operation not permitted"
// without, whatever the process's user.
func (p Probe) AgentGrant() Grant {
	f, err := os.Open(p.TCCPath)
	if err == nil {
		_ = f.Close()
		return GrantGiven
	}
	if errors.Is(err, fs.ErrPermission) {
		return GrantMissing
	}
	return GrantUnknown
}

// ExtensionRefusal is why macOS last refused the extension an Endpoint Security client, or "" when the extension recorded no refusal
// or its record cannot be read.
func (p Probe) ExtensionRefusal() string {
	data, err := os.ReadFile(p.StatusPath)
	if err != nil {
		return ""
	}
	var record struct {
		Refused string `json:"refused"`
	}
	if json.Unmarshal(data, &record) != nil {
		return ""
	}
	return record.Refused
}
