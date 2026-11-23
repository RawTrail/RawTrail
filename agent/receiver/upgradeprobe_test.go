package receiver

import "testing"

// TestParseNEUpgradePending pins the systemextensionsctl-output parsing that distinguishes a staged upgrade (a previous NE
// version "waiting to uninstall on reboot" beside an enabled new one) from every other state, so the agent only emits the
// reboot-required hint when a reboot is genuinely the fix (#399, #1598). Both markers must land on lines that name OUR
// network-extension bundle.
func TestParseNEUpgradePending(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name: "staged upgrade: old version waiting to uninstall on reboot",
			output: "2 extension(s)\n" +
				"--- com.apple.system_extension.network_extension\n" +
				"enabled\tactive\tteamID\tbundleID (version)\tname\t[state]\n" +
				"*\t*\tFDG8Q7N4CC\tio.rawtrail.networkextension (0.2.1/1)\tRawTrail Network Extension\t[activated enabled]\n" +
				"\t*\tFDG8Q7N4CC\tio.rawtrail.networkextension (0.1.1/1)\tnetworkextension\t[terminated waiting to uninstall on reboot]\n",
			want: true,
		},
		{
			name: "single active version: no pending uninstall",
			output: "1 extension(s)\n" +
				"*\t*\tFDG8Q7N4CC\tio.rawtrail.networkextension (0.2.1/1)\tRawTrail Network Extension\t[activated enabled]\n",
			want: false,
		},
		{
			name: "not approved yet: NE present but no reboot marker",
			output: "1 extension(s)\n" +
				"\t\tFDG8Q7N4CC\tio.rawtrail.networkextension (0.2.1/1)\tRawTrail Network Extension\t[activated waiting for user]\n",
			want: false,
		},
		{
			// spec:agent-xpc-receiver/distinct-signal-when-a-sysext-upgrade-leaves-the-ne-registration-stale/awaiting-approval-is-not-a-reboot
			//
			// Issue #1598, as a v0.9.0 laptop upgrade over two rc installs left it: the old versions wait for the reboot whatever
			// else is going on, and approving the new one is what connected the agent.
			name: "new version waiting for approval beside old versions pending removal",
			output: "3 extension(s)\n" +
				"--- com.apple.system_extension.network_extension\n" +
				"enabled\tactive\tteamID\tbundleID (version)\tname\t[state]\n" +
				"\t*\tFDG8Q7N4CC\tio.rawtrail.networkextension (v0.9.0-rc.10/1)\tRawTrail Network Extension\t" +
				"[terminated waiting to uninstall on reboot]\n" +
				"\t*\tFDG8Q7N4CC\tio.rawtrail.networkextension (v0.9.0-rc.9/1)\tRawTrail Network Extension\t" +
				"[terminated waiting to uninstall on reboot]\n" +
				"\t\tFDG8Q7N4CC\tio.rawtrail.networkextension (v0.9.0/1)\tRawTrail Network Extension\t[activated waiting for user]\n",
			want: false,
		},
		{
			// spec:agent-xpc-receiver/distinct-signal-when-a-sysext-upgrade-leaves-the-ne-registration-stale/switched-off-is-not-a-reboot
			name: "new version switched off beside an old version pending removal",
			output: "2 extension(s)\n" +
				"\t*\tFDG8Q7N4CC\tio.rawtrail.networkextension (0.1.1/1)\tnetworkextension\t[terminated waiting to uninstall on reboot]\n" +
				"\t*\tFDG8Q7N4CC\tio.rawtrail.networkextension (0.2.1/1)\tRawTrail Network Extension\t[activated disabled]\n",
			want: false,
		},
		{
			name: "another vendor's enabled NE does not stand in for ours",
			output: "2 extension(s)\n" +
				"*\t*\tOTHERTEAM\tcom.othervendor.networkextension (1.0/1)\tOther\t[activated enabled]\n" +
				"\t*\tFDG8Q7N4CC\tio.rawtrail.networkextension (0.1.1/1)\tnetworkextension\t[terminated waiting to uninstall on reboot]\n",
			want: false,
		},
		{
			name: "a different vendor's NE waiting to uninstall must not match",
			output: "1 extension(s)\n" +
				"\t*\tOTHERTEAM\tcom.othervendor.networkextension (1.0/1)\tOther\t[terminated waiting to uninstall on reboot]\n",
			want: false,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseNEUpgradePending(tc.output, neExtensionBundleID); got != tc.want {
				t.Fatalf("parseNEUpgradePending = %v, want %v", got, tc.want)
			}
		})
	}
}
