// laptop_test holds the laptop install (issue #1435) to what it promises a person running RawTrail on their own Mac: a stack nothing
// else on the network can reach, sized for a laptop, and an installer that pins the agent to the server it made and never repoints
// or removes an agent that reports somewhere else. The compose checks read the shipped profile; the installer checks run the real
// script against stubbed system tools, with its system paths under a scratch directory.
package arch_test

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const (
	laptopCompose   = "../../docker-compose.laptop.yml"
	laptopInstaller = "../../scripts/laptop/rawtrail-laptop"
	laptopURL       = "https://localhost:8443"
	// setupLink is the one-time admin setup link the stub server prints in its log, in the server's banner format.
	setupLink = laptopURL + "/admin/break-glass/setup?token=Zm9vYmFy_setup-token"
)

type laptopDoc struct {
	Services map[string]laptopService `json:"services"`
}

type laptopService struct {
	Ports       []string          `json:"ports"`
	Environment map[string]string `json:"environment"`
	MemLimit    string            `json:"mem_limit"`
	Volumes     []string          `json:"volumes"`
}

func readLaptopCompose(t *testing.T) laptopDoc {
	t.Helper()
	raw, err := os.ReadFile(laptopCompose)
	require.NoError(t, err)
	var doc laptopDoc
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	require.Contains(t, doc.Services, "server")
	require.Contains(t, doc.Services, "clickhouse")
	return doc
}

// spec:release-packaging/the-laptop-stack-is-reachable-from-its-own-mac-only/only-the-server-is-published-on-the-loopback-interface
func TestLaptopStackPublishesOnlyTheServerOnLoopback(t *testing.T) {
	t.Parallel()
	for name, svc := range readLaptopCompose(t).Services {
		if name != "server" {
			assert.Empty(t, svc.Ports, "%s publishes a host port", name)
			continue
		}
		require.Len(t, svc.Ports, 1)
		assert.True(t, strings.HasPrefix(svc.Ports[0], "127.0.0.1:"), "the server is published as %q, not on 127.0.0.1", svc.Ports[0])
	}
}

// spec:release-packaging/the-laptop-stack-is-reachable-from-its-own-mac-only/the-laptop-server-exports-no-telemetry
func TestLaptopServerExportsNoTelemetryAndServesItsOwnTLS(t *testing.T) {
	t.Parallel()
	env := readLaptopCompose(t).Services["server"].Environment
	assert.NotContains(t, env, "OTEL_EXPORTER_OTLP_ENDPOINT")
	assert.NotEmpty(t, env["EDR_TLS_CERT_FILE"])
	assert.NotEmpty(t, env["EDR_TLS_KEY_FILE"])
	assert.NotContains(t, env, "EDR_TLS_TERMINATED_BY_PROXY")
}

// spec:release-packaging/the-laptop-stack-is-sized-for-a-laptop/clickhouse-refuses-a-query-before-the-kernel-kills-it
func TestLaptopClickHouseCapsItselfBelowItsContainer(t *testing.T) {
	t.Parallel()
	clickhouse := readLaptopCompose(t).Services["clickhouse"]
	var mounted string
	for _, v := range clickhouse.Volumes {
		if strings.Contains(v, "/etc/clickhouse-server/config.d/") {
			mounted = strings.SplitN(v, ":", 2)[0]
		}
	}
	require.NotEmpty(t, mounted, "ClickHouse mounts no config of its own")
	//nolint:gosec // G304: the path is the compose file's own mount, inside this repository.
	raw, err := os.ReadFile(filepath.Join("..", "..", mounted))
	require.NoError(t, err)
	var config struct {
		MaxServerMemoryUsage int64 `xml:"max_server_memory_usage"`
	}
	require.NoError(t, xml.Unmarshal(raw, &config))
	require.Positive(t, config.MaxServerMemoryUsage)
	assert.LessOrEqual(t, config.MaxServerMemoryUsage, int64(1_100_000_000))
	assert.Less(t, config.MaxServerMemoryUsage, megabytes(t, clickhouse.MemLimit),
		"the container limit must sit above ClickHouse's own cap, or the kernel kills it first")
}

// megabytes reads a compose memory size such as 1280m as bytes.
func megabytes(t *testing.T, size string) int64 {
	t.Helper()
	require.True(t, strings.HasSuffix(size, "m"), "mem_limit %q is not in megabytes", size)
	n, err := strconv.ParseInt(strings.TrimSuffix(size, "m"), 10, 64)
	require.NoError(t, err)
	return n * 1024 * 1024
}

// spec:release-packaging/the-laptop-stack-is-sized-for-a-laptop/events-and-process-graphs-expire-together-after-a-week
func TestLaptopKeepsEventsAndGraphsAWeek(t *testing.T) {
	t.Parallel()
	env := readLaptopCompose(t).Services["server"].Environment
	assert.Equal(t, "7", env["EDR_EVENT_RETENTION_DAYS"])
	assert.Equal(t, "7", env["EDR_RETENTION_DAYS"])
}

// laptopRun is one sandbox the installer runs in: stubbed system tools first on PATH, a scratch home for its state, and a scratch
// root for /etc, /var and /Library. Every stub appends its command line to calls.
type laptopRun struct {
	dir, stubs, state, root, calls string
}

// laptopStubs are the system tools the installer calls, each recording how it was called. docker answers info as an engine
// that is running unless NO_ENGINE is set, records the image compose was given, and prints the server's setup banner for compose
// logs unless NO_BANNER is set; spctl rejects a package when NOT_NOTARIZED is set; installer fails when INSTALL_FAILS is set;
// security fails when TRUST_DECLINED is set; sudo runs its command as the test user, and fails its up-front check when SUDO_REFUSED
// is set.
var laptopStubs = map[string]string{
	"uname": `echo Darwin`,
	"docker": `case "$1 $2" in
  "info --format") [ "$3" = "{{.MemTotal}}" ] && echo "${ENGINE_MEMORY:-8000000000}" || echo "Docker Desktop" ;;
  "info ") [ -z "$NO_ENGINE" ] ;;
  "inspect -f") echo 2026-10-31T12:00:00Z ;;
  *)
    [ "$1" != compose ] || echo "image EDR_VERSION=$EDR_VERSION" >> "$CALLS"
    case "$*" in
      *" ps -q "*) echo server-container ;;
      *" logs "*) [ -n "$NO_BANNER" ] || printf '  Email: admin@localhost\n  URL:   ` + setupLink + `\n  TTL:   1h0m0s\n' ;;
    esac ;;
esac`,
	"curl": `for a in "$@"; do
  case "$a" in -sfI) echo "location: https://github.com/RawTrail/RawTrail/releases/tag/v9.9.9"; exit 0 ;; esac
done
out=""; while [ $# -gt 0 ]; do [ "$1" = -o ] && out="$2"; shift; done
[ -z "$out" ] || [ "$out" = /dev/null ] && exit 0
# A dropped download leaves part of the file behind and fails, as curl does.
if [ -n "$DOWNLOAD_FAILS" ]; then echo pa > "$out"; exit 56; fi
echo pkg > "$out"`,
	"sudo":      `[ "$1" = -v ] && exit "${SUDO_REFUSED:-0}"; exec "$@"`,
	"spctl":     `[ -z "$NOT_NOTARIZED" ]`,
	"installer": `[ -z "$INSTALL_FAILS" ]`,
	"launchctl": `:`,
	"open":      `:`,
	"security":  `[ -z "$TRUST_DECLINED" ] || [ "$1" != add-trusted-cert ]`,
	"sleep":     `:`,
}

func newLaptopRun(t *testing.T) laptopRun {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the installer is a bash script")
	}
	dir := t.TempDir()
	run := laptopRun{dir: dir, stubs: filepath.Join(dir, "bin"), state: filepath.Join(dir, "state"), root: filepath.Join(dir, "root"),
		calls: filepath.Join(dir, "calls")}
	require.NoError(t, os.MkdirAll(run.stubs, 0o750))
	for name, body := range laptopStubs {
		script := fmt.Sprintf("#!/bin/bash\necho \"%s $*\" >> %q\n%s\n", name, run.calls, body)
		//nolint:gosec // G306: a stub must be executable.
		require.NoError(t, os.WriteFile(filepath.Join(run.stubs, name), []byte(script), 0o750))
	}
	uninstaller := filepath.Join(run.root, "Library", "Application Support", "io.rawtrail", "uninstall.sh")
	require.NoError(t, os.MkdirAll(filepath.Dir(uninstaller), 0o750))
	// Like the package's own, the stub removes itself when it finishes and stays for a retry when it cannot (UNINSTALL_STUCK).
	stub := fmt.Appendf(nil, "#!/bin/bash\necho \"uninstall.sh\" >> %q\n[ -n \"$UNINSTALL_STUCK\" ] || rm -f \"$0\"\n", run.calls)
	//nolint:gosec // G306: the stub uninstaller must be executable.
	require.NoError(t, os.WriteFile(uninstaller, stub, 0o750))
	return run
}

// run invokes the installer with args, the extra environment, and PATH limited to the stubs and the base system, so a developer's
// own docker or mkcert is never reached. It returns the combined output and whether it exited zero.
func (r laptopRun) run(t *testing.T, env []string, args ...string) (string, bool) {
	t.Helper()
	//nolint:gosec // G204: the installer path is this repository's and the arguments are the test's own.
	cmd := exec.CommandContext(t.Context(), "bash", append([]string{laptopInstaller}, args...)...)
	cmd.Env = append([]string{"PATH=" + r.stubs + ":/usr/bin:/bin", "HOME=" + r.dir, "RAWTRAIL_LAPTOP_HOME=" + r.state,
		"RAWTRAIL_LAPTOP_ROOT=" + r.root, "CALLS=" + r.calls}, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func (r laptopRun) callLog(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(r.calls)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(raw)
}

func (r laptopRun) conf() string { return filepath.Join(r.root, "etc", "rawtrail.conf") }

func (r laptopRun) writeConf(t *testing.T, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(r.conf()), 0o750))
	require.NoError(t, os.WriteFile(r.conf(), []byte(content), 0o600))
}

func (r laptopRun) pkg(t *testing.T) string {
	t.Helper()
	pkg := filepath.Join(r.dir, "rawtrail-v9.9.9.pkg")
	require.NoError(t, os.WriteFile(pkg, []byte("pkg"), 0o600))
	return pkg
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	//nolint:gosec // G304: the path is under the test's own scratch directory.
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(raw)
}

// certificateSHA256 is the hex SHA-256 of the certificate's DER bytes, read with Go's own parser rather than the openssl pipeline
// the installer uses, so the pin is checked against an independent reading of the certificate.
func certificateSHA256(t *testing.T, path string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(readFile(t, path)))
	require.NotNil(t, block, "%s holds no PEM certificate", path)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	assert.Contains(t, cert.DNSNames, "localhost")
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// spec:release-packaging/one-command-installs-the-laptop-stack-and-the-agent/no-docker-engine-is-running
func TestLaptopInstallRefusesWithoutADockerEngine(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, []string{"NO_ENGINE=1"}, "install", "--pkg", r.pkg(t))
	require.False(t, ok)
	assert.Contains(t, out, "no Docker engine is running")
	assert.NotContains(t, r.callLog(t), "compose")
	assert.NoFileExists(t, r.conf())
}

// spec:release-packaging/one-command-installs-the-laptop-stack-and-the-agent/the-agent-config-pins-the-server-certificate
func TestLaptopInstallPinsTheAgentToItsServer(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, nil, "install", "--pkg", r.pkg(t))
	require.True(t, ok, out)

	want := fmt.Sprintf("EDR_SERVER_URL=%s\nEDR_ENROLL_SECRET=%s\nEDR_SERVER_FINGERPRINT=%s\n", laptopURL,
		readFile(t, filepath.Join(r.state, "secrets", "enroll_secret")), certificateSHA256(t, filepath.Join(r.state, "tls", "server.crt")))
	assert.Equal(t, want, readFile(t, r.conf()))
	info, err := os.Stat(r.conf())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	// The server loads the pair with Go's TLS stack, which refuses keys some openssl builds write.
	_, err = tls.LoadX509KeyPair(filepath.Join(r.state, "tls", "server.crt"), filepath.Join(r.state, "tls", "server.key"))
	require.NoError(t, err)

	calls := r.callLog(t)
	// The installer runs on a root-owned copy of the given package, the same copy Gatekeeper checked.
	assert.Contains(t, calls, "cp "+filepath.Join(r.dir, "rawtrail-v9.9.9.pkg")+" ")
	assert.Regexp(t, `spctl --assess --type install (\S+)/agent\.pkg\n(?s:.*)installer -pkg (\S+)/agent\.pkg -target /`, calls)
	assert.Contains(t, calls, "open "+setupLink+"\n")
	// Without mkcert the certificate is self-signed and trusted in the login keychain, for the passkey page's secure context.
	assert.Contains(t, calls, "security add-trusted-cert -r trustRoot -p ssl")
	// The config is in place before the package's postinstall starts the agent.
	assert.Less(t, strings.Index(calls, "sh -c"), strings.Index(calls, "installer -pkg"))
}

// spec:release-packaging/one-command-installs-the-laptop-stack-and-the-agent/a-rerun-keeps-the-secrets
func TestLaptopInstallRerunKeepsSecrets(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, nil, "install", "--no-agent")
	require.True(t, ok, out)
	kept := []string{
		"secrets/mysql_root", "secrets/secret_key", "secrets/enroll_secret", "secrets/edr_dsn", "tls/server.crt", "tls/server.key",
	}
	before := map[string]string{}
	for _, f := range kept {
		before[f] = readFile(t, filepath.Join(r.state, f))
		assert.NotEmpty(t, before[f], f)
	}
	assert.Contains(t, before["secrets/edr_dsn"], "root:"+before["secrets/mysql_root"]+"@tcp(mysql:3306)/edr")

	out, ok = r.run(t, nil, "install", "--no-agent")
	require.True(t, ok, out)
	for _, f := range kept {
		assert.Equal(t, before[f], readFile(t, filepath.Join(r.state, f)), "%s changed on a rerun", f)
	}
}

// spec:release-packaging/one-command-installs-the-laptop-stack-and-the-agent/a-package-that-is-not-notarized-is-not-installed
func TestLaptopInstallRefusesAPackageThatIsNotNotarized(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, []string{"NOT_NOTARIZED=1"}, "install", "--pkg", r.pkg(t))
	require.False(t, ok)
	assert.Contains(t, out, "is not notarized")
	assert.NotContains(t, r.callLog(t), "installer -pkg")
}

const otherServerConf = "EDR_SERVER_URL=https://edr.example.com\nEDR_ENROLL_SECRET=theirs\n"

// spec:release-packaging/the-laptop-install-leaves-another-server-s-agent-alone/an-agent-reporting-elsewhere-is-not-repointed-unasked
func TestLaptopInstallDoesNotRepointAnotherServersAgentUnasked(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	r.writeConf(t, otherServerConf)
	out, ok := r.run(t, nil, "install", "--pkg", r.pkg(t))
	require.False(t, ok)
	assert.Contains(t, out, "https://edr.example.com")
	assert.Contains(t, out, "--replace-config")
	assert.Equal(t, otherServerConf, readFile(t, r.conf()))
	assert.NotContains(t, r.callLog(t), "installer -pkg")
}

// spec:release-packaging/the-laptop-install-leaves-another-server-s-agent-alone/a-replaced-config-is-restored-on-uninstall
func TestLaptopInstallRestoresAReplacedConfigOnUninstall(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	r.writeConf(t, otherServerConf)
	enrolled := filepath.Join(r.root, "var", "db", "rawtrail", "enrolled.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(enrolled), 0o750))
	require.NoError(t, os.WriteFile(enrolled, []byte("token"), 0o600))

	out, ok := r.run(t, nil, "install", "--pkg", r.pkg(t), "--replace-config")
	require.True(t, ok, out)
	assert.Contains(t, readFile(t, r.conf()), "EDR_SERVER_URL="+laptopURL+"\n")
	assert.NoFileExists(t, enrolled, "a token bound to the other server's URL must go, so the agent enrolls with this one")

	out, ok = r.run(t, nil, "uninstall")
	require.True(t, ok, out)
	assert.Equal(t, otherServerConf, readFile(t, r.conf()))
}

// spec:release-packaging/the-laptop-install-leaves-another-server-s-agent-alone/uninstall-leaves-another-server-s-agent-installed
func TestLaptopUninstallLeavesAnotherServersAgent(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, nil, "install", "--no-agent")
	require.True(t, ok, out)
	r.writeConf(t, otherServerConf)

	out, ok = r.run(t, nil, "uninstall")
	require.True(t, ok, out)
	assert.NotContains(t, r.callLog(t), "uninstall.sh")
	assert.Equal(t, otherServerConf, readFile(t, r.conf()))
}

// A sudo that cannot run must stop the uninstall, not read as a Mac with no agent config and leave the agent behind.
func TestLaptopUninstallStopsWhenSudoCannotRun(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, nil, "install", "--pkg", r.pkg(t))
	require.True(t, ok, out)

	out, ok = r.run(t, []string{"SUDO_REFUSED=1"}, "uninstall")
	require.False(t, ok)
	assert.Contains(t, out, "need sudo")
	assert.NotContains(t, r.callLog(t), "down -v")
	assert.FileExists(t, r.conf())
}

// With no agent config on the Mac there is nothing of root's to read, so a server-only install uninstalls without sudo.
func TestLaptopUninstallNeedsNoSudoWithoutAnAgentConfig(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, nil, "install", "--no-agent")
	require.True(t, ok, out)

	out, ok = r.run(t, []string{"SUDO_REFUSED=1"}, "uninstall")
	require.True(t, ok, out)
	assert.Contains(t, r.callLog(t), "down -v")
	assert.NoDirExists(t, r.state)
}

// spec:release-packaging/uninstall-removes-what-the-laptop-install-added/uninstall-removes-the-agent-the-stack-and-the-state
func TestLaptopUninstallRemovesWhatTheInstallAdded(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, nil, "install", "--pkg", r.pkg(t))
	require.True(t, ok, out)

	out, ok = r.run(t, nil, "uninstall")
	require.True(t, ok, out)
	calls := r.callLog(t)
	assert.Contains(t, calls, "uninstall.sh\n")
	assert.Contains(t, calls, "down -v --remove-orphans")
	assert.Contains(t, calls, "security remove-trusted-cert")
	assert.NoFileExists(t, r.conf())
	assert.NoDirExists(t, r.state)
}

// spec:release-packaging/one-command-installs-the-laptop-stack-and-the-agent/an-existing-config-is-rewritten-private
func TestLaptopInstallRewritesAnExistingConfigPrivate(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	r.writeConf(t, "EDR_SERVER_URL="+laptopURL+"\n")
	require.NoError(t, os.Chmod(r.conf(), 0o644)) //nolint:gosec // G302: the world-readable config this test starts from.
	out, ok := r.run(t, nil, "install", "--pkg", r.pkg(t))
	require.True(t, ok, out)
	info, err := os.Stat(r.conf())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.Contains(t, readFile(t, r.conf()), "EDR_ENROLL_SECRET=")
}

// spec:release-packaging/one-command-installs-the-laptop-stack-and-the-agent/a-declined-certificate-trust-is-asked-again
func TestLaptopInstallAsksAgainForADeclinedTrust(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, []string{"TRUST_DECLINED=1"}, "install", "--no-agent")
	require.False(t, ok)
	assert.Contains(t, out, "was not trusted")
	cert := readFile(t, filepath.Join(r.state, "tls", "server.crt"))

	out, ok = r.run(t, nil, "install", "--no-agent")
	require.True(t, ok, out)
	assert.Equal(t, 2, strings.Count(r.callLog(t), "security add-trusted-cert"))
	assert.Equal(t, cert, readFile(t, filepath.Join(r.state, "tls", "server.crt")), "the retry must trust the same certificate")
	assert.FileExists(t, filepath.Join(r.state, "tls", "trusted-in-keychain"))
}

// spec:release-packaging/one-command-installs-the-laptop-stack-and-the-agent/a-server-whose-admin-is-set-up-opens-the-sign-in-page
func TestLaptopOpensTheSignInPageWhenNoSetupLinkWasPrinted(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, []string{"NO_BANNER=1"}, "install", "--no-agent")
	require.True(t, ok, out)
	calls := r.callLog(t)
	assert.Contains(t, calls, "open "+laptopURL+"\n")
	assert.NotContains(t, calls, "open "+setupLink)
	// The link is read from the running server's current start only.
	assert.Contains(t, calls, "logs --no-log-prefix --since 2026-10-31T12:00:00Z server")
}

// spec:release-packaging/one-command-installs-the-laptop-stack-and-the-agent/login-keeps-the-installed-server-image
func TestLaptopLoginKeepsTheInstalledImage(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, nil, "install", "--no-agent", "--version", "v1.2.3")
	require.True(t, ok, out)
	require.NoError(t, os.Remove(r.calls))

	out, ok = r.run(t, nil, "login")
	require.True(t, ok, out)
	calls := r.callLog(t)
	assert.Contains(t, calls, "restart server")
	assert.NotContains(t, calls, " up ")
	assert.Contains(t, calls, "image EDR_VERSION=v1.2.3\n")
	assert.NotContains(t, calls, "image EDR_VERSION=latest")
}

// spec:release-packaging/the-laptop-install-leaves-another-server-s-agent-alone/a-failed-package-install-leaves-the-agent-as-it-was
func TestLaptopFailedPackageLeavesTheAgentAsItWas(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		env  string
	}{
		{"package rejected by Gatekeeper", "NOT_NOTARIZED=1"},
		{"package fails to install", "INSTALL_FAILS=1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newLaptopRun(t)
			r.writeConf(t, otherServerConf)
			enrolled := filepath.Join(r.root, "var", "db", "rawtrail", "enrolled.plist")
			require.NoError(t, os.MkdirAll(filepath.Dir(enrolled), 0o750))
			require.NoError(t, os.WriteFile(enrolled, []byte("token"), 0o600))

			out, ok := r.run(t, []string{tc.env}, "install", "--pkg", r.pkg(t), "--replace-config")
			require.False(t, ok, out)
			assert.Equal(t, otherServerConf, readFile(t, r.conf()))
			assert.FileExists(t, enrolled)
		})
	}
}

// spec:release-packaging/the-laptop-install-leaves-another-server-s-agent-alone/the-agent-config-is-read-as-the-agent-reads-it
func TestLaptopReadsTheAgentConfigAsTheAgentDoes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		conf    string
		removed bool
	}{
		{"spaces around the equals sign", "# laptop\n  EDR_SERVER_URL = " + laptopURL + "  \n", true},
		{"lowercase key", "edr_server_url=" + laptopURL + "\n", true},
		{"quoted value", "EDR_SERVER_URL='" + laptopURL + "'\n", true},
		{"a later entry for another server wins", "EDR_SERVER_URL=" + laptopURL + "\nEDR_SERVER_URL=https://edr.example.com\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newLaptopRun(t)
			out, ok := r.run(t, nil, "install", "--no-agent")
			require.True(t, ok, out)
			r.writeConf(t, tc.conf)

			out, ok = r.run(t, nil, "uninstall")
			require.True(t, ok, out)
			if tc.removed {
				assert.Contains(t, r.callLog(t), "uninstall.sh\n")
				assert.NoFileExists(t, r.conf())
			} else {
				assert.NotContains(t, r.callLog(t), "uninstall.sh")
				assert.Equal(t, tc.conf, readFile(t, r.conf()))
			}
		})
	}
}

// spec:release-packaging/uninstall-removes-what-the-laptop-install-added/uninstall-waits-for-the-agent-s-extensions-to-go
func TestLaptopUninstallWaitsForTheExtensionsToGo(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, nil, "install", "--pkg", r.pkg(t))
	require.True(t, ok, out)

	out, ok = r.run(t, []string{"UNINSTALL_STUCK=1"}, "uninstall")
	require.False(t, ok)
	assert.Contains(t, out, "run rawtrail-laptop uninstall again")
	assert.FileExists(t, r.conf())
	assert.NotContains(t, r.callLog(t), "down -v")
	assert.DirExists(t, r.state)
}

// spec:release-packaging/uninstall-removes-what-the-laptop-install-added/an-existing-directory-is-never-adopted
func TestLaptopInstallNeverAdoptsAnExistingDirectory(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	require.NoError(t, os.MkdirAll(r.state, 0o750))
	keep := filepath.Join(r.state, "notes.txt")
	require.NoError(t, os.WriteFile(keep, []byte("mine"), 0o600))

	out, ok := r.run(t, nil, "install", "--no-agent")
	require.False(t, ok)
	assert.Contains(t, out, "is not a laptop install's")
	assert.NoFileExists(t, filepath.Join(r.state, ".rawtrail-laptop"))
	assert.Equal(t, "mine", readFile(t, keep))
}

// A Docker engine with too little memory for the stack is named before anything starts.
func TestLaptopInstallWarnsAboutAnEngineShortOfMemory(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, []string{"ENGINE_MEMORY=2000000000"}, "install", "--no-agent")
	require.True(t, ok, out)
	assert.Contains(t, out, "the Docker engine has 2000 MB of memory")

	out, ok = r.run(t, nil, "install", "--no-agent")
	require.True(t, ok, out)
	assert.NotContains(t, out, "MB of memory")
}

// A dropped download is fetched again on the next run, rather than kept and refused as a package that is not notarized.
func TestLaptopInstallFetchesAnInterruptedDownloadAgain(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	out, ok := r.run(t, []string{"DOWNLOAD_FAILS=1"}, "install")
	require.False(t, ok)
	assert.Contains(t, out, "could not download the agent package for v9.9.9")
	downloads := filepath.Join(r.state, "downloads")
	entries, err := os.ReadDir(downloads)
	require.NoError(t, err)
	assert.Empty(t, entries, "a partial download must not be left where the next run would take it as complete")

	out, ok = r.run(t, nil, "install")
	require.True(t, ok, out)
	assert.Equal(t, "pkg\n", readFile(t, filepath.Join(downloads, "rawtrail-v9.9.9.pkg")))
}

// spec:release-packaging/uninstall-removes-what-the-laptop-install-added/uninstall-removes-nothing-from-a-directory-no-install-made
func TestLaptopUninstallNeedsTheInstallsStateDirectory(t *testing.T) {
	t.Parallel()
	r := newLaptopRun(t)
	r.writeConf(t, "EDR_SERVER_URL="+laptopURL+"\n")
	out, ok := r.run(t, nil, "uninstall")
	require.False(t, ok)
	assert.Contains(t, out, "is not a laptop install's state directory")
	calls := r.callLog(t)
	assert.NotContains(t, calls, "uninstall.sh")
	assert.NotContains(t, calls, "down -v")
	assert.FileExists(t, r.conf())
}
