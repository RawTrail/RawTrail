//go:build windows

package enrich

// ReadLaunchdJobs reads nothing on Windows, which has no launchd.
func ReadLaunchdJobs(string, int, int) []LaunchdJob { return nil }
