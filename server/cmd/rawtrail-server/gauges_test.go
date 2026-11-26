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

// activeHosts is the endpoint context as the fleet gauges see it: the non-revoked enrollments.
type activeHosts struct {
	endpointapi.Service
	ids []string
	err error
}

func (a activeHosts) ActiveHostIDs(context.Context) ([]string, error) { return a.ids, a.err }

// onlineHosts is the detection context as the fleet gauges see it: the hosts seen within the threshold it was asked about.
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
func TestHostFleetCountsOfflineOnlyAmongActiveEnrollments(t *testing.T) {
	t.Parallel()
	const enrolledUpper, seenLower = "93DFC6F5-0000-4000-8000-00000000000A", "93dfc6f5-0000-4000-8000-00000000000a"
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
		// Enrollment accepts either case, and a re-enrollment in the other case keeps the first row; MySQL matches them, so must this.
		{"a host seen under the other case of its UUID is online", []string{enrolledUpper}, []string{seenLower}, 0},
		{"whichever side holds which case", []string{seenLower}, []string{enrolledUpper}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotThreshold time.Duration
			src := serverGaugeSource{
				endpointSvc:  activeHosts{ids: tc.active},
				detectionSvc: onlineHosts{ids: tc.online, gotThreshold: &gotThreshold},
			}

			fleet, err := src.HostFleet(t.Context(), metricsOfflineThreshold)

			require.NoError(t, err)
			assert.Equal(t, tc.want, fleet.Offline)
			assert.Equal(t, len(tc.active), fleet.Enrolled, "enrolled is the same read of the enrollments")
			assert.Equal(t, metricsOfflineThreshold, gotThreshold, "the threshold reaches detection")
		})
	}
}

// A failing source must surface rather than read as zero offline hosts, which is what the gauge callback logs and skips on.
func TestHostFleetSurfacesEitherSourcesFailure(t *testing.T) {
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
			_, err := tc.src.HostFleet(t.Context(), time.Minute)
			require.ErrorIs(t, err, tc.want)
		})
	}
}
