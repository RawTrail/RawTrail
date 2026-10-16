//go:build windows

package enrich

// ReadLaunchdJob reads nothing on Windows, which has no launchd.
func ReadLaunchdJob(string, int, int) (LaunchdJob, bool) { return LaunchdJob{}, false }
