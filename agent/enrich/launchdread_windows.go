//go:build windows

package enrich

// ReadLaunchdJobs reads nothing on Windows, which has no launchd.
func ReadLaunchdJobs(string, int, int, int) ([]LaunchdJob, int) { return nil, 1 }
