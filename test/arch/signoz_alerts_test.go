package arch_test

// signoz_alerts_test checks the committed SigNoz alert rules against the code they watch. A rule that names a metric the server no
// longer emits does not fail in SigNoz: it evaluates to no data and stays quiet forever, which reads exactly like a healthy system.

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const alertRulesDir = "config/observability/alerts"

var (
	// metricLiteralRE matches an EDR metric name as the instrument constructors receive it: a quoted string literal.
	metricLiteralRE = regexp.MustCompile(`"(edr\.[a-z0-9_.]+)"`)
	// histogramRE finds a histogram registration, whose name SigNoz exposes only with a series suffix (.bucket, .count, ...).
	histogramRE = regexp.MustCompile(`(?:Float64|Int64)Histogram\(\s*"(edr\.[a-z0-9_.]+)"`)
	// serviceFilterRE pulls the service a rule's query is scoped to.
	serviceFilterRE = regexp.MustCompile(`service\.name = '([a-z-]+)'`)
)

// serviceSourceRoots maps each OTel service.name to the source trees its binary is built from, so a rule is checked against the
// metrics that service emits rather than against the whole repository: the agent and the server share metric names.
var serviceSourceRoots = map[string][]string{
	"fleet-edr-server": {"server", "internal"},
	"fleet-edr-agent":  {"agent", "internal"},
}

type alertRule struct {
	Alert     string            `json:"alert"`
	Labels    map[string]string `json:"labels"`
	Condition struct {
		CompositeQuery struct {
			Queries []struct {
				Spec struct {
					Signal       string `json:"signal"`
					Aggregations []struct {
						MetricName string `json:"metricName"`
					} `json:"aggregations"`
					Filter *struct {
						Expression string `json:"expression"`
					} `json:"filter"`
				} `json:"spec"`
			} `json:"queries"`
		} `json:"compositeQuery"`
	} `json:"condition"`
}

// spec:observability-instrumentation/shipped-alert-rules-read-emitted-metrics/a-rule-reads-a-metric-its-service-emits
func TestSigNozAlertRules_WatchMetricsTheirServiceEmits(t *testing.T) {
	t.Parallel()
	repo := os.DirFS(repoRootFromTest(t))
	emitted := make(map[string]emittedMetrics, len(serviceSourceRoots))
	for service, roots := range serviceSourceRoots {
		emitted[service] = emittedMetricNames(t, repo, roots)
	}

	files, err := fs.Glob(repo, path.Join(alertRulesDir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "no alert rules under %s", alertRulesDir)

	names := make(map[string]string)
	for _, file := range files {
		raw, err := fs.ReadFile(repo, file)
		require.NoError(t, err)
		var rule alertRule
		require.NoError(t, json.Unmarshal(raw, &rule), file)
		previous, duplicate := names[rule.Alert]
		assert.False(t, duplicate, "%s and %s share the name %q, and the apply script matches rules by name", previous, file, rule.Alert)
		names[rule.Alert] = file

		t.Run(path.Base(file), func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, []string{"critical", "error", "warning", "info"}, rule.Labels["severity"],
				"routing policies match on the severity label")
			for _, query := range rule.Condition.CompositeQuery.Queries {
				if query.Spec.Signal == "" {
					continue // a formula: it reads the builder queries, which are checked on their own
				}
				require.NotNil(t, query.Spec.Filter, "every query is scoped to one service and environment")
				service := serviceFilterRE.FindStringSubmatch(query.Spec.Filter.Expression)
				require.NotNil(t, service, "every query filters on service.name")
				serviceMetrics, known := emitted[service[1]]
				require.True(t, known, "unknown service %q", service[1])
				// tools/signoz-alerts.sh rewrites this literal to the operator's environment, so it must appear verbatim.
				assert.Contains(t, query.Spec.Filter.Expression, "deployment.environment = 'prod'")
				for _, aggregation := range query.Spec.Aggregations {
					if !strings.HasPrefix(aggregation.MetricName, "edr.") {
						continue // a trace count, or a metric from the shared instrumentation (db.sql.*, http.server.*)
					}
					assert.Contains(t, serviceMetrics.names, serviceMetrics.registeredName(aggregation.MetricName),
						"%s does not emit the metric this rule watches", service[1])
				}
			}
		})
	}
}

// emittedMetricNames collects every edr.* metric name written as a string literal under roots, outside tests.
// histogramSeries are the suffixes SigNoz appends to an OTel histogram's name: a rule reads edr.detection.lag.bucket for a
// percentile and edr.detection.lag.count for a rate, while the code registers edr.detection.lag.
var histogramSeries = []string{".bucket", ".count", ".sum", ".min", ".max"}

// emittedMetrics is what a service's source registers: every edr.* name, and which of them are histograms.
type emittedMetrics struct {
	names      map[string]bool
	histograms map[string]bool
}

// registeredName returns the registered name behind a SigNoz histogram series, or name unchanged. A suffix is stripped only when
// the name before it is a registered histogram, so a rule reading edr.events.ingested.count is still refused: that counter is
// exposed under its own name.
func (m emittedMetrics) registeredName(name string) string {
	for _, suffix := range histogramSeries {
		if base, ok := strings.CutSuffix(name, suffix); ok && m.histograms[base] {
			return base
		}
	}
	return name
}

func emittedMetricNames(t *testing.T, repo fs.FS, roots []string) emittedMetrics {
	t.Helper()
	names := make(map[string]bool)
	histograms := make(map[string]bool)
	for _, root := range roots {
		err := fs.WalkDir(repo, root, func(file string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
				return err
			}
			source, err := fs.ReadFile(repo, file)
			if err != nil {
				return err
			}
			for _, match := range metricLiteralRE.FindAllStringSubmatch(string(source), -1) {
				names[match[1]] = true
			}
			for _, match := range histogramRE.FindAllStringSubmatch(string(source), -1) {
				histograms[match[1]] = true
			}
			return nil
		})
		require.NoError(t, err)
	}
	return emittedMetrics{names: names, histograms: histograms}
}

// A dashboard query that names a temporality reads only that kind, so a deployment switching to delta would see its panels go
// empty; and a compose that does not forward the variable leaves the switch unreachable from .env.
//
// spec:observability-instrumentation/metric-temporality-is-selectable-per-backend/compose-forwards-it-and-dashboards-do-not-pin-it
func TestTemporalityPreference_ForwardedAndNotPinned(t *testing.T) {
	t.Parallel()
	repo := os.DirFS(repoRootFromTest(t))

	compose, err := fs.ReadFile(repo, "docker-compose.quickstart.yml")
	require.NoError(t, err)
	assert.Contains(t, string(compose),
		"OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE: ${OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE:-}")

	dashboards, err := fs.Glob(repo, "config/observability/*.json")
	require.NoError(t, err)
	require.NotEmpty(t, dashboards)
	for _, dashboard := range dashboards {
		raw, err := fs.ReadFile(repo, dashboard)
		require.NoError(t, err)
		assert.NotContains(t, string(raw), `"temporality"`, "%s pins a temporality", dashboard)
	}
}
