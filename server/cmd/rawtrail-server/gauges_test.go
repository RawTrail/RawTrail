package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	detectionapi "github.com/rawtrail/rawtrail/server/detection/api"
	endpointapi "github.com/rawtrail/rawtrail/server/endpoint/api"
)

// activeHosts is the endpoint context as the offline gauge sees it: the non-revoked enrollments.
type activeHosts struct {
	endpointapi.Service
	ids []string
	err error
}

func (a activeHosts) ActiveHostIDs(context.Context) ([]string, error) { return a.ids, a.err }

// onlineHosts is the detection context as the offline gauge sees it: the hosts seen within the threshold it was asked about.
type onlineHosts struct {
	detectionapi.Service
	ids          []string
	err          error
	gotThreshold *time.Duration
}

func (o onlineHosts) OnlineHostIDs(_ context.Context, threshold time.Duration) ([]string, error) {
	*o.gotThreshold = threshold
	return o.ids, o.err
}

// Offline hosts are counted among the non-revoked enrollments only. A revoked host keeps its detection row and stops checking in, and
// counting it held "EDR hosts offline" at 200% on the dogfood server and let revoked hosts silence "EDR event ingest stopped" (#1624).
//
// spec:observability-instrumentation/observable-host-fleet-gauges/a-revoked-host-is-not-counted-offline
func TestOfflineHostsCountsOnlyActiveEnrollments(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		active []string
		online []string
		want   int
	}{
		{"a revoked host that stopped checking in is not counted", []string{"online-host"}, []string{"online-host"}, 0},
		{"an enrolled host not seen within the threshold is", []string{"online-host", "silent-host"}, []string{"online-host"}, 1},
		{"so is an enrollment that never checked in", []string{"never-seen"}, nil, 1},
		{"a host seen but revoked since is not", nil, []string{"revoked-but-polling"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotThreshold time.Duration
			src := serverGaugeSource{
				endpointSvc:  activeHosts{ids: tc.active},
				detectionSvc: onlineHosts{ids: tc.online, gotThreshold: &gotThreshold},
			}

			offline, err := src.OfflineHosts(t.Context(), metricsOfflineThreshold)

			require.NoError(t, err)
			assert.Equal(t, tc.want, offline)
			assert.LessOrEqual(t, offline, len(tc.active), "offline hosts are a subset of enrolled ones")
			assert.Equal(t, metricsOfflineThreshold, gotThreshold, "the threshold reaches detection")
		})
	}
}

// A failing source must surface rather than read as zero offline hosts, which is what the gauge callback logs and skips on.
func TestOfflineHostsSurfacesEitherSourcesFailure(t *testing.T) {
	t.Parallel()
	endpointDown := errors.New("endpoint store down")
	detectionDown := errors.New("detection store down")
	cases := []struct {
		name string
		src  serverGaugeSource
		want error
	}{
		{"endpoint", serverGaugeSource{
			endpointSvc:  activeHosts{err: endpointDown},
			detectionSvc: onlineHosts{gotThreshold: new(time.Duration)},
		}, endpointDown},
		{"detection", serverGaugeSource{
			endpointSvc:  activeHosts{ids: []string{"host-a"}},
			detectionSvc: onlineHosts{err: detectionDown, gotThreshold: new(time.Duration)},
		}, detectionDown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.src.OfflineHosts(t.Context(), time.Minute)
			require.ErrorIs(t, err, tc.want)
		})
	}
}
