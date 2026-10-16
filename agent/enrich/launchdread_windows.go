//go:build windows

package enrich

// ReadLaunchdJob reads nothing on Windows, which has no launchd.
func ReadLaunchdJob(string, int) (LaunchdJob, bool) { return LaunchdJob{}, false }
