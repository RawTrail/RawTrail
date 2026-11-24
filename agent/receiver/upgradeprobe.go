package receiver

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// neExtensionBundleID is the network extension's bundle identifier as systemextensionsctl reports it (NOT the app-group Mach
// service name the agent connects to). A staged upgrade leaves the previous version of this bundle waiting to uninstall on
// reboot while the new version is active, which strands the NE's Mach service registration on the dead version (#399).
const neExtensionBundleID = "io.rawtrail.networkextension"

// uninstallOnRebootMarker is the state phrase systemextensionsctl prints for a version macOS will remove on the next reboot.
const uninstallOnRebootMarker = "waiting to uninstall on reboot"

// activatedEnabledMarker is the state systemextensionsctl prints for a version macOS has activated and the person has allowed. Any
// other state of the newest version, such as "[activated waiting for user]" before the person approves it or "[activated
// disabled]" once they switch it off, is a reason it cannot connect that a restart does not fix (issue #1598).
const activatedEnabledMarker = "[activated enabled]"

// systemextensionsctlPath is the ABSOLUTE path to the system-extension CLI. Invoking it by absolute path (not a bare command
// resolved via PATH) both hardens the root LaunchDaemon against a hijacked / writable PATH entry (gosec G204, Sonar go:S4036)
// and guarantees the probe runs under the minimal PATH a LaunchDaemon inherits, where a bare "systemextensionsctl" could fail
// and silently suppress the reboot hint.
const systemextensionsctlPath = "/usr/bin/systemextensionsctl"

// launchctlPath is the absolute path to launchctl, for the same reasons as systemextensionsctlPath.
const launchctlPath = "/bin/launchctl"

// diagnosticsMarker is what names this product's extensions, their launchd jobs and their Mach services in the output
// NEUpgradeDiagnostics reads: io.rawtrail.networkextension, group.io.rawtrail.networkextension, and the security extension's.
const diagnosticsMarker = "io.rawtrail."

// neUpgradeProbeTimeout bounds the probe so a hung systemextensionsctl cannot stall the receiver's connect loop: the probe is
// called synchronously from the loop goroutine, so an unbounded exec would freeze reconnect/heartbeat for that service.
const neUpgradeProbeTimeout = 5 * time.Second

// NEUpgradePending reports whether the OS has a previous network-extension version staged for removal on reboot while the new one
// is activated and enabled, the condition that strands the NE's Mach service registration on the dead version until reboot (#399).
// Best-effort: any error running
// systemextensionsctl (including on non-darwin, where the binary is absent) returns false, so the agent falls back to the
// generic connect-failure warning rather than a misleading reboot hint. Wired as the network-extension Loop's UpgradeProbe;
// the ESF loop does not use it.
func NEUpgradePending(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, neUpgradeProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, systemextensionsctlPath, "list").CombinedOutput()
	if err != nil {
		return false
	}
	return parseNEUpgradePending(string(out), neExtensionBundleID)
}

// NEUpgradeDiagnostics returns what the host shows about this product's extensions as a stale upgrade is reported (issue #1542):
// their systemextensionsctl lines, with each version's state, and the launchd lines naming them, which say whether the network
// extension's Mach service is registered. On the Mac that needed a reboot the service was missing while both extensions read
// activated enabled. Best-effort: a tool that fails or times out contributes a line saying so rather than nothing.
func NEUpgradeDiagnostics(ctx context.Context) []string {
	run := func(name string, args ...string) []string {
		ctx, cancel := context.WithTimeout(ctx, neUpgradeProbeTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput() // #nosec G204 -- absolute paths and constant arguments
		if err != nil {
			return []string{fmt.Sprintf("%s %s: %v", name, strings.Join(args, " "), err)}
		}
		return productLines(string(out))
	}
	return append(run(systemextensionsctlPath, "list"), run(launchctlPath, "print", "system")...)
}

// productLines returns the lines of output that name this product, trimmed, in order.
func productLines(output string) []string {
	var lines []string
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, diagnosticsMarker) {
			lines = append(lines, strings.Join(strings.Fields(line), " "))
		}
	}
	return lines
}

// parseNEUpgradePending is the pure core: true when one line names the network-extension bundle with the
// waiting-to-uninstall-on-reboot marker AND another names it activated and enabled. Per-line matching on those stable tokens
// isolates the old, terminated version from the new one and stays robust to the surrounding columns (team id, version, display
// name, enabled/active flags) that vary across macOS versions and install states.
//
// The old version alone is not enough. It stays registered until the next reboot whatever else is going on, so it is also there
// while the new version waits for the person to approve it, and a restart is then the wrong instruction: approving it is what
// connects the agent (issue #1598). Only an enabled new version that the agent still cannot reach is the stranded registration
// a restart finishes.
func parseNEUpgradePending(systemextensionsctlOutput, bundleID string) bool {
	var oldPendingRemoval, newEnabled bool
	for line := range strings.SplitSeq(systemextensionsctlOutput, "\n") {
		if !strings.Contains(line, bundleID) {
			continue
		}
		oldPendingRemoval = oldPendingRemoval || strings.Contains(line, uninstallOnRebootMarker)
		newEnabled = newEnabled || strings.Contains(line, activatedEnabledMarker)
	}
	return oldPendingRemoval && newEnabled
}
