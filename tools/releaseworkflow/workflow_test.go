// Package releaseworkflow tests the release workflow's publishing steps by running their scripts, read out of
// .github/workflows/release.yml, under bash with the runner's -e. gh and cosign are stubs that record their calls, so nothing reaches
// GitHub, Sigstore or a real release. The point is what the steps do when a run is repeated or fails part way (issue #1215).
package releaseworkflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

const workflowPath = "../../.github/workflows/release.yml"

const (
	createStep   = "Create GitHub Release (draft)"
	signStep     = "Sign release artifacts (cosign keyless)"
	attestStep   = "Attest pkg + profiles + SBOMs + SHA256SUMS"
	publishStep  = "Publish the release"
	teardownStep = "Teardown keychain"
	pkgJob       = "macos-pkg"
)

type step struct {
	ID              string `yaml:"id"`
	Name            string `yaml:"name"`
	If              string `yaml:"if"`
	Run             string `yaml:"run"`
	Uses            string `yaml:"uses"`
	ContinueOnError bool   `yaml:"continue-on-error"`
}

func pkgSteps(t *testing.T) []step {
	t.Helper()
	raw, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	var wf struct {
		Jobs map[string]struct {
			Steps []step `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &wf))
	steps := wf.Jobs[pkgJob].Steps
	require.NotEmpty(t, steps, "no steps in job %s", pkgJob)
	return steps
}

func stepIndex(t *testing.T, steps []step, name string) int {
	t.Helper()
	i := slices.IndexFunc(steps, func(s step) bool { return s.Name == name })
	require.GreaterOrEqual(t, i, 0, "no step %q in job %s", name, pkgJob)
	return i
}

// ghStub records each call in gh.log. The releases list answers from FAKE_RELEASE: "none" is no release for the tag, "draft" one draft,
// "published" one public release, and "error" is a lookup that fails.
const ghStub = `#!/usr/bin/env bash
echo "gh $*" >> "$RUNNER_TEMP/gh.log"
if [ "$1" = "api" ]; then
  case "$FAKE_RELEASE" in
    draft) echo true ;;
    published) echo false ;;
    error) echo "HTTP 502" >&2; exit 1 ;;
  esac
fi
`

// cosignStub writes the bundle a real sign-blob would, named by --bundle.
const cosignStub = `#!/usr/bin/env bash
echo "cosign $*" >> "$RUNNER_TEMP/cosign.log"
while [ $# -gt 0 ]; do
  if [ "$1" = "--bundle" ]; then echo '{}' > "$2"; fi
  shift
done
`

// writeExecutable writes a stub the script will exec. Go takes syscall.ForkLock exclusively around a fork, so holding it shared across
// the write means no forked test child inherits the writable descriptor and an exec of the stub cannot fail with ETXTBSY.
func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	syscall.ForkLock.RLock()
	err := os.WriteFile(path, []byte(content), 0o700) //nolint:gosec // a test stub that must be executable
	syscall.ForkLock.RUnlock()
	require.NoError(t, err)
}

type run struct {
	dir  string
	temp string
	out  string
	err  error
}

func (r run) log(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(r.temp, name)) //nolint:gosec // a file the stubs wrote in this test's temp dir
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// runStep executes the named step's script in a scratch working directory holding the release's dist files.
func runStep(t *testing.T, name, tag, fakeRelease string) run {
	t.Helper()
	var script string
	for _, s := range pkgSteps(t) {
		if s.Name == name {
			script = s.Run
		}
	}
	require.NotEmpty(t, script, "step %q has no script", name)

	dir, temp := t.TempDir(), t.TempDir()
	bin := filepath.Join(temp, "bin")
	require.NoError(t, os.Mkdir(bin, 0o750))
	writeExecutable(t, filepath.Join(bin, "gh"), ghStub)
	writeExecutable(t, filepath.Join(bin, "cosign"), cosignStub)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "dist"), 0o750))
	for _, f := range []string{
		"rawtrail-" + tag + ".pkg", "edr-system-extension.mobileconfig", "edr-tcc-fda.mobileconfig", "edr-login-items.mobileconfig",
		"rawtrail-" + tag + "-sbom.spdx.json", "rawtrail-" + tag + "-sbom.cdx.json", "SHA256SUMS",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "dist", f), []byte("x"), 0o600))
	}

	//nolint:gosec // runs this repository's own workflow script, which is what is under test
	cmd := exec.CommandContext(t.Context(), "bash", "-e", "-c", script)
	cmd.Dir = dir
	cmd.Env = []string{
		"HOME=" + temp, "RUNNER_TEMP=" + temp, "RELEASE_TAG=" + tag, "GITHUB_REPOSITORY=rawtrail/rawtrail", "GH_TOKEN=token", "FAKE_RELEASE=" + fakeRelease,
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), //nolint:forbidigo // the script needs bash and coreutils
	}
	out, err := cmd.CombinedOutput()
	return run{dir: dir, temp: temp, out: string(out), err: err}
}

func assets(tag string) []string {
	return []string{
		"dist/rawtrail-" + tag + ".pkg", "dist/edr-system-extension.mobileconfig", "dist/edr-tcc-fda.mobileconfig",
		"dist/edr-login-items.mobileconfig", "dist/rawtrail-" + tag + "-sbom.spdx.json", "dist/rawtrail-" + tag + "-sbom.cdx.json",
		"dist/SHA256SUMS",
	}
}

// The release starts as a draft, so a run that fails before the end leaves nothing public.
//
// spec:release-packaging/a-release-is-published-only-when-complete-and-its-build-can-be-repeated/a-failed-run-leaves-only-a-draft
func TestCreateStep_StartsADraft(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		tag            string
		wantPrerelease bool
	}{
		{"a release candidate is a draft and a prerelease", "v0.9.0-rc.1", true},
		{"a general release is a draft and not a prerelease", "v0.9.0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := runStep(t, createStep, tc.tag, "none")
			require.NoError(t, r.err, r.out)

			calls := r.log(t, "gh.log")
			require.Len(t, calls, 2, "a look-up of the existing releases, then one create: %v", calls)
			create := calls[1]
			assert.True(t, strings.HasPrefix(create, "gh release create "+tc.tag+" "), create)
			assert.Contains(t, create, " --draft ")
			assert.Equal(t, tc.wantPrerelease, strings.Contains(create, " --prerelease "), create)
			for _, a := range assets(tc.tag) {
				assert.Contains(t, create, a)
			}
			assert.NotContains(t, strings.Join(calls, "\n"), "--draft=false", "nothing publishes at this step")
		})
	}
}

// A re-run finds the draft the failed run left, and replaces every artifact on it together.
//
// spec:release-packaging/a-release-is-published-only-when-complete-and-its-build-can-be-repeated/a-re-run-reuses-the-draft
func TestCreateStep_ReusesAnExistingDraft(t *testing.T) {
	t.Parallel()
	r := runStep(t, createStep, "v0.9.0-rc.1", "draft")
	require.NoError(t, r.err, r.out)

	calls := r.log(t, "gh.log")
	require.Len(t, calls, 2, "%v", calls)
	assert.True(t, strings.HasPrefix(calls[1], "gh release upload v0.9.0-rc.1 --clobber "), calls[1])
	for _, a := range assets("v0.9.0-rc.1") {
		assert.Contains(t, calls[1], a, "every artifact is replaced, so the set stays consistent")
	}
	assert.NotContains(t, strings.Join(calls, "\n"), "release create")
}

// A release that is already public is not repaired by uploading over it: the bytes users may have downloaded would change.
//
// spec:release-packaging/a-release-is-published-only-when-complete-and-its-build-can-be-repeated/a-published-release-is-not-overwritten
func TestCreateStep_RefusesAPublishedRelease(t *testing.T) {
	t.Parallel()
	r := runStep(t, createStep, "v0.9.0-rc.1", "published")
	require.Error(t, r.err)
	assert.Contains(t, r.out, "already published")
	calls := r.log(t, "gh.log")
	require.Len(t, calls, 1, "only the look-up ran: %v", calls)
	assert.True(t, strings.HasPrefix(calls[0], "gh api "), calls[0])
}

// A look-up that fails says nothing about whether a release exists, so the step stops instead of creating over one.
//
// spec:release-packaging/a-release-is-published-only-when-complete-and-its-build-can-be-repeated/a-published-release-is-not-overwritten
func TestCreateStep_StopsWhenTheLookUpFails(t *testing.T) {
	t.Parallel()
	r := runStep(t, createStep, "v0.9.0-rc.1", "error")
	require.Error(t, r.err)
	assert.Contains(t, r.out, "Stopping rather than assuming none exists")
	calls := r.log(t, "gh.log")
	require.Len(t, calls, 1, "neither a create nor an upload ran: %v", calls)
}

// The bundles replace any a failed run uploaded, so a re-run does not stop on the first one that is already there.
//
// spec:release-packaging/a-release-is-published-only-when-complete-and-its-build-can-be-repeated/a-re-run-reuses-the-draft
func TestSignStep_UploadsTheBundlesOverAnEarlierRun(t *testing.T) {
	t.Parallel()
	r := runStep(t, signStep, "v0.9.0-rc.1", "draft")
	require.NoError(t, r.err, r.out)

	signed := r.log(t, "cosign.log")
	assert.Len(t, signed, 7, "every artifact is signed")
	calls := r.log(t, "gh.log")
	require.Len(t, calls, 1, "%v", calls)
	assert.True(t, strings.HasPrefix(calls[0], "gh release upload v0.9.0-rc.1 --clobber "), calls[0])
	assert.Equal(t, 7, strings.Count(calls[0], ".sigstore.json"), "all seven bundles go up together: %s", calls[0])
}

// The release is published by the last step, so it goes public only once every artifact and bundle is on it.
//
// spec:release-packaging/a-release-is-published-only-when-complete-and-its-build-can-be-repeated/a-complete-release-is-published
func TestPublishStep_IsTheLastOneAndLeavesAPrereleaseOutOfLatest(t *testing.T) {
	t.Parallel()
	steps := pkgSteps(t)
	publish := stepIndex(t, steps, publishStep)
	assert.Greater(t, publish, stepIndex(t, steps, createStep))
	assert.Greater(t, publish, stepIndex(t, steps, signStep), "after the bundles are uploaded")
	assert.Greater(t, publish, stepIndex(t, steps, attestStep), "after the provenance is attested")
	assert.Equal(t, publish+1, stepIndex(t, steps, teardownStep), "nothing but the keychain teardown follows")
	for i, s := range steps {
		if i != publish {
			assert.NotContains(t, s.Run, "--draft=false", "step %q must not publish", s.Name)
		}
	}

	for _, tc := range []struct {
		tag        string
		wantLatest bool
	}{
		{"v0.9.0-rc.1", false},
		{"v0.9.0", true},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			t.Parallel()
			r := runStep(t, publishStep, tc.tag, "draft")
			require.NoError(t, r.err, r.out)
			calls := r.log(t, "gh.log")
			require.Len(t, calls, 1, "%v", calls)
			assert.True(t, strings.HasPrefix(calls[0], "gh release edit "+tc.tag+" --draft=false"), calls[0])
			assert.Equal(t, tc.wantLatest, strings.Contains(calls[0], "--latest"), calls[0])
		})
	}
}

// The cosign installer downloads from GitHub at run time, so a transient 5xx would fail the job. Each attempt runs only when the one
// before failed, and the last one is allowed to fail the job.
//
// spec:release-packaging/a-release-is-published-only-when-complete-and-its-build-can-be-repeated/the-cosign-install-is-retried
func TestCosignInstallIsRetried(t *testing.T) {
	t.Parallel()
	steps := pkgSteps(t)
	first := stepIndex(t, steps, "Install cosign")
	second := stepIndex(t, steps, "Install cosign (retry)")
	third := stepIndex(t, steps, "Install cosign (last retry)")
	require.Equal(t, []int{first + 1, first + 2}, []int{second, third}, "the attempts are consecutive")
	assert.Less(t, third, stepIndex(t, steps, signStep), "all attempts come before signing")

	assert.True(t, steps[first].ContinueOnError, "a failed first attempt must not fail the job")
	assert.True(t, steps[second].ContinueOnError)
	assert.False(t, steps[third].ContinueOnError, "the last attempt is the one that fails the job")
	assert.Contains(t, steps[second].If, steps[first].ID+".outcome == 'failure'")
	assert.Contains(t, steps[third].If, steps[second].ID+".outcome == 'failure'")
	assert.Contains(t, steps[first].If, "startsWith(github.ref, 'refs/tags/v')", "a dry-run never installs it")
	assert.Equal(t, steps[first].Uses, steps[second].Uses, "the same pinned installer each time")
	assert.Equal(t, steps[first].Uses, steps[third].Uses)
}

// The Go test jobs must run for a change to the release workflow alone, or the tests above never see the scripts they pin: test.yml's
// change detection skips them for a pull request that touches no Go file.
func TestChangeDetectionRunsTheGoTestsForAReleaseWorkflowChange(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../.github/workflows/test.yml")
	require.NoError(t, err)
	var wf struct {
		Jobs map[string]struct {
			Steps []step `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &wf))
	var script string
	for _, s := range wf.Jobs["changes"].Steps {
		if s.ID == "detect" {
			script = s.Run
		}
	}
	require.NotEmpty(t, script, "no detect step in test.yml")

	temp := t.TempDir()
	bin := filepath.Join(temp, "bin")
	require.NoError(t, os.Mkdir(bin, 0o750))
	writeExecutable(t, filepath.Join(bin, "gh"), "#!/usr/bin/env bash\nprintf '.github/workflows/release.yml\\n'\n")
	out := filepath.Join(temp, "out")
	require.NoError(t, os.WriteFile(out, nil, 0o600))

	//nolint:gosec // runs this repository's own workflow script, which is what is under test
	cmd := exec.CommandContext(t.Context(), "bash", "-c", script)
	cmd.Env = []string{
		"HOME=" + temp, "GITHUB_OUTPUT=" + out, "EVENT_NAME=pull_request", "PR_NUMBER=1", "REPO=rawtrail/rawtrail",
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), //nolint:forbidigo // the script needs bash and grep
	}
	combined, err := cmd.CombinedOutput()
	require.NoError(t, err, string(combined))
	got, err := os.ReadFile(out) //nolint:gosec // a file the script wrote in this test's temp dir
	require.NoError(t, err)
	assert.Contains(t, strings.Split(string(got), "\n"), "go=true", string(combined))
}
