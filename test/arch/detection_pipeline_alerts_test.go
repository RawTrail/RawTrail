package arch_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pipelineAlertRule is the part of a SigNoz rule these tests pin: the queries, the formula, the threshold, the window and the
// severity, which together are the alert's contract with an operator.
type pipelineAlertRule struct {
	Alert     string `json:"alert"`
	Condition struct {
		CompositeQuery struct {
			Queries []struct {
				Type string `json:"type"`
				Spec struct {
					Name         string `json:"name"`
					Expression   string `json:"expression"`
					Disabled     bool   `json:"disabled"`
					Aggregations []struct {
						MetricName       string `json:"metricName"`
						TimeAggregation  string `json:"timeAggregation"`
						SpaceAggregation string `json:"spaceAggregation"`
					} `json:"aggregations"`
				} `json:"spec"`
			} `json:"queries"`
		} `json:"compositeQuery"`
		SelectedQueryName string `json:"selectedQueryName"`
		Thresholds        struct {
			Spec []struct {
				Target    float64 `json:"target"`
				Op        string  `json:"op"`
				MatchType string  `json:"matchType"`
			} `json:"spec"`
		} `json:"thresholds"`
	} `json:"condition"`
	Labels     map[string]string `json:"labels"`
	Evaluation struct {
		Spec struct {
			EvalWindow string `json:"evalWindow"`
		} `json:"spec"`
	} `json:"evaluation"`
}

func loadPipelineAlertRule(t *testing.T, file string) pipelineAlertRule {
	t.Helper()
	raw, err := fs.ReadFile(os.DirFS(repoRootFromTest(t)), path.Join(alertRulesDir, file))
	require.NoError(t, err)
	var rule pipelineAlertRule
	require.NoError(t, json.Unmarshal(raw, &rule))
	require.Len(t, rule.Condition.Thresholds.Spec, 1)
	return rule
}

// spec:observability-instrumentation/stalled-and-slow-detection-raise-shipped-alerts/a-stalled-pipeline-raises-a-critical-alert
//
// The stalled rule reads the lag rate and the backlog, and fires on the formula that is non-zero only while the backlog is up and
// no lag is recorded. A missing lag sample counts as zero in SigNoz, which is what makes "records nothing" detectable at all.
func TestDetectionStalledAlertRule(t *testing.T) {
	t.Parallel()
	rule := loadPipelineAlertRule(t, "edr-detection-stalled.json")

	assert.Equal(t, "EDR detection stalled", rule.Alert)
	assert.Equal(t, "critical", rule.Labels["severity"])
	assert.Equal(t, "15m", rule.Evaluation.Spec.EvalWindow)
	queries := rule.Condition.CompositeQuery.Queries
	require.Len(t, queries, 3)
	assert.Equal(t, "edr.detection.lag.count", queries[0].Spec.Aggregations[0].MetricName)
	assert.Equal(t, "rate", queries[0].Spec.Aggregations[0].TimeAggregation)
	assert.Equal(t, "edr.events.pending", queries[1].Spec.Aggregations[0].MetricName)
	assert.Equal(t, "F1", rule.Condition.SelectedQueryName)
	assert.Equal(t, "builder_formula", queries[2].Type)
	assert.Equal(t, "A == 0 ? B : 0", queries[2].Spec.Expression, "zero while the backlog is zero, so an idle pipeline does not page")
	threshold := rule.Condition.Thresholds.Spec[0]
	assert.Equal(t, "above", threshold.Op)
	assert.Zero(t, threshold.Target)
	assert.Equal(t, "all_the_times", threshold.MatchType, "the condition must hold for the whole window, not one sample")
}

// spec:observability-instrumentation/stalled-and-slow-detection-raise-shipped-alerts/slow-detection-raises-an-error-alert
//
// The lag rule reads the histogram's p95 and fires only when it stays above five minutes for the whole ten-minute window.
func TestDetectionLagHighAlertRule(t *testing.T) {
	t.Parallel()
	rule := loadPipelineAlertRule(t, "edr-detection-lag-high.json")

	assert.Equal(t, "EDR detection lag high", rule.Alert)
	assert.Equal(t, "error", rule.Labels["severity"])
	assert.Equal(t, "10m", rule.Evaluation.Spec.EvalWindow)
	queries := rule.Condition.CompositeQuery.Queries
	require.Len(t, queries, 1)
	assert.Equal(t, "edr.detection.lag.bucket", queries[0].Spec.Aggregations[0].MetricName)
	assert.Equal(t, "p95", queries[0].Spec.Aggregations[0].SpaceAggregation)
	assert.Equal(t, "A", rule.Condition.SelectedQueryName)
	threshold := rule.Condition.Thresholds.Spec[0]
	assert.Equal(t, "above", threshold.Op)
	assert.InDelta(t, 300.0, threshold.Target, 0, "five minutes, in the histogram's unit of seconds")
	assert.Equal(t, "all_the_times", threshold.MatchType)
}
