package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"github.com/rawtrail/rawtrail/internal/control"
)

// What an operator actually sees about a control connection is its span, and these tests read the recorded span rather than the
// handler's return value (issue #1124).
//
// The distinction is the whole reason this file exists. A client that goes away takes the transport with it, and gRPC ends the RPC
// with the transport's own error whatever the handler returned, so a handler that returns nil is NOT sufficient to record an
// ordinary disconnect as successful. The first version of this fix changed only the handler, passed its unit tests, and still
// produced `Error: transport is closing` on edr-dev.
//
// So the gateway is served here the way production serves it, over net/http's HTTP/2 server rather than bufconn, and the client's
// socket is killed rather than closed politely. Both details matter: they are what reproduces the failure.

// recordedConn is one client connection to a recorded gateway: the stream, and the raw socket underneath it so a test can make the
// client vanish the way a killed agent does.
type recordedConn struct {
	stream control.ControlChannel_ConnectClient
	socket net.Conn
}

// recordedGateway serves a gateway over cleartext HTTP/2 with an in-memory span recorder, and returns the recorder, the gateway,
// and a dialer.
func recordedGateway(t *testing.T, ver *fakeVerifier) (*tracetest.SpanRecorder, *Gateway, func(token string, opts ...grpc.CallOption) recordedConn) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	// The gateway's OWN server, with only the tracer provider substituted. Building a server here instead would leave New's wrapping
	// untested, and New's wrapping is the fix.
	g := New(Deps{
		Source:   newFakeSource(),
		Verifier: ver,
		Stats:    otelgrpc.NewServerHandler(otelgrpc.WithTracerProvider(provider)),
	})
	g.livenessInterval = 20 * time.Millisecond
	g.revocationInterval = 20 * time.Millisecond
	srv := g.GRPCServer()

	lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	httpSrv := &http.Server{Handler: srv, Protocols: protocols, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = httpSrv.Serve(lis) }()
	t.Cleanup(func() { _ = httpSrv.Close() })

	dial := func(token string, opts ...grpc.CallOption) recordedConn {
		t.Helper()
		var socket net.Conn
		cc, err := grpc.NewClient(lis.Addr().String(),
			grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
				c, derr := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
				socket = c
				return c, derr
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = cc.Close() })
		stream, err := control.NewControlChannelClient(cc).Connect(connectCtx(token), opts...)
		require.NoError(t, err)
		return recordedConn{stream: stream, socket: socket}
	}
	return recorder, g, dial
}

// connectStatus waits for the Connect span to be recorded and returns the status it carries, which is the whole of what an operator
// reads about how that connection ended.
func connectStatus(t *testing.T, recorder *tracetest.SpanRecorder) sdktrace.Status {
	t.Helper()
	var recorded sdktrace.Status
	require.Eventually(t, func() bool {
		for _, s := range recorder.Ended() {
			if s.Name() == "rawtrail.control.v1.ControlChannel/Connect" {
				recorded = s.Status()
				return true
			}
		}
		return false
	}, 10*time.Second, 20*time.Millisecond, "the connection's span was never recorded")
	return recorded
}

// The headline. Every host disconnects routinely, so recording each as a failure left this operation permanently error-coloured and
// a genuine control-path fault indistinguishable from the background.
//
// spec:agent-control-channel/how-a-connection-ended-is-what-its-telemetry-reports/a-client-that-disconnects-ends-its-connection-cleanly
func TestAnOrdinaryDisconnectIsNotRecordedAsAFailedConnection(t *testing.T) {
	t.Parallel()
	ver := newFakeVerifier()
	ver.add("tok-a", "host-a")
	recorder, g, dial := recordedGateway(t, ver)

	conn := dial("tok-a")
	require.Eventually(t, func() bool { return g.reg.get("host-a") != nil }, 5*time.Second, 10*time.Millisecond)

	// Killed, not closed: a restarted or suspended agent does not get to send a GOAWAY, and a polite close does not reproduce this.
	require.NoError(t, conn.socket.Close())

	recorded := connectStatus(t, recorder)
	assert.NotEqual(t, otelcodes.Error, recorded.Code,
		"an agent restart, a closed lid or a dropped link is how a control connection ordinarily ends")
	assert.Empty(t, recorded.Description)
}

// A failed receive is read from its status code, not from the context: gRPC cancels the stream context moments after it rejects a
// frame, so a cancelled context says only that the stream is over. A transport ending under the stream is a bare error, or under
// net/http's HTTP/2 server an Unavailable or Canceled status (issue #1329); a status with any other code is gRPC rejecting a frame
// (issue #1405) and stays a fault whether or not the context has been cancelled yet.
//
// spec:agent-control-channel/a-status-failure-after-a-disconnect-ends-cleanly/an-unavailable-status-is-a-disconnect
// spec:agent-control-channel/a-status-failure-after-a-disconnect-ends-cleanly/a-rejected-frame-is-a-fault
func TestEndAfterReceiveReadsTheStatusCode(t *testing.T) {
	t.Parallel()
	clientDisconnected := status.Error(codes.Unavailable, "client disconnected")
	clientCanceled := status.Error(codes.Canceled, "body closed by handler")
	oversized := status.Error(codes.ResourceExhausted, "grpc: received message larger than max")
	unreadable := status.Error(codes.Internal, "grpc: failed to unmarshal the received message")

	cases := []struct {
		name         string
		recvErr      error
		ctxCancelled bool
		wantErr      error
	}{
		{"an Unavailable status is a client that went away", clientDisconnected, false, nil},
		{"an Unavailable status with the context cancelled is a client that went away", clientDisconnected, true, nil},
		{"a Canceled status is a client that went away", clientCanceled, false, nil},
		{"a bare transport error is a client that went away", errors.New("transport is closing"), false, nil},
		{"a clean half-close is an ordinary end", nil, false, nil},
		{"an oversized frame is a fault", oversized, false, oversized},
		{"an unreadable frame is a fault", unreadable, false, unreadable},
		// gRPC cancels the stream context after it writes the rejection, so the fault must survive a cancelled context.
		{"an unreadable frame is a fault even once the context is cancelled", unreadable, true, unreadable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			if tc.ctxCancelled {
				cancel()
			}
			assert.Equal(t, tc.wantErr, endAfterReceive(ctx, newConn("host-a", "tok-a", func() {}), tc.recvErr))
		})
	}
}

// garbageCodec marshals every message to bytes no AgentFrame can be unmarshalled from, so the server rejects the frame as unreadable.
type garbageCodec struct{}

func (garbageCodec) Marshal(any) ([]byte, error) {
	return []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, nil
}
func (garbageCodec) Unmarshal([]byte, any) error { return nil }
func (garbageCodec) Name() string                { return "proto" }

// A frame the server cannot read is recorded as a failed connection, through the real gateway served over net/http's HTTP/2 server.
// The first version of the disconnect fix waited for the stream context to be cancelled before reporting a status, and gRPC cancels it
// moments after rejecting a frame, so this ended as a clean connection until a test sent a real one.
//
// spec:agent-control-channel/how-a-connection-ended-is-what-its-telemetry-reports/a-receive-failure-is-still-recorded-as-a-failure
func TestAnUnreadableFrameIsRecordedAsAFailedConnection(t *testing.T) {
	t.Parallel()
	ver := newFakeVerifier()
	ver.add("tok-a", "host-a")
	recorder, g, dial := recordedGateway(t, ver)

	conn := dial("tok-a", grpc.ForceCodec(garbageCodec{}))
	require.Eventually(t, func() bool { return g.reg.get("host-a") != nil }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, conn.stream.SendMsg(&control.AgentFrame{}))

	recorded := connectStatus(t, recorder)
	assert.Equal(t, otelcodes.Error, recorded.Code, "a frame this server cannot read is a fault an operator wants to find")
	assert.Contains(t, recorded.Description, "unmarshal")
}

// An oversized frame is rejected with ResourceExhausted, which OpenTelemetry's gRPC convention does not colour as a server error, so
// the span stays unset. What matters is that the RPC ends with that status, which the span carries as rpc.response.status_code and the
// client receives.
//
// spec:agent-control-channel/how-a-connection-ended-is-what-its-telemetry-reports/a-receive-failure-is-still-recorded-as-a-failure
func TestAnOversizedFrameEndsTheRPCWithResourceExhausted(t *testing.T) {
	t.Parallel()
	ver := newFakeVerifier()
	ver.add("tok-a", "host-a")
	recorder, g, dial := recordedGateway(t, ver)

	conn := dial("tok-a")
	require.Eventually(t, func() bool { return g.reg.get("host-a") != nil }, 5*time.Second, 10*time.Millisecond)
	_ = conn.stream.Send(&control.AgentFrame{Frame: &control.AgentFrame_Outcome{Outcome: &control.Outcome{
		Id: 1, Status: "completed", Result: make([]byte, 5<<20),
	}}})

	// Heartbeats tick every 20 ms here, so one can already be buffered ahead of the rejection: drain them to the end of the stream.
	err := recvEviction(t, conn.stream)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err), "the client is told why its frame was refused")
	connectStatus(t, recorder)
	for _, span := range recorder.Ended() {
		if span.Name() != "rawtrail.control.v1.ControlChannel/Connect" {
			continue
		}
		var code string
		for _, attr := range span.Attributes() {
			if string(attr.Key) == "rpc.response.status_code" {
				code = attr.Value.AsString()
			}
		}
		assert.Equal(t, "RESOURCE_EXHAUSTED", code, "the span names the status the RPC ended with")
	}
}

// The other half: the wrapper must not swallow the connections an operator is looking for. A teardown the server initiated is one
// of those, and its span has to name which teardown it was.
//
// spec:agent-control-channel/how-a-connection-ended-is-what-its-telemetry-reports/a-server-teardown-still-tells-the-client-to-reconnect
func TestAServerTeardownIsStillRecordedAsAFailedConnection(t *testing.T) {
	t.Parallel()
	ver := newFakeVerifier()
	ver.add("tok-a", "host-a")
	recorder, g, dial := recordedGateway(t, ver)

	dial("tok-a")
	require.Eventually(t, func() bool { return g.reg.get("host-a") != nil }, 5*time.Second, 10*time.Millisecond)

	ver.revoke("tok-a") // the maintenance re-check tears the connection down

	recorded := connectStatus(t, recorder)
	assert.Equal(t, otelcodes.Error, recorded.Code, "a connection the server ended is the case an operator wants to find")
	assert.Contains(t, recorded.Description, string(reasonTokenInvalid))
}

// recordingStats captures the events the wrapper forwards, so a test can see what the OTel handler underneath would have been given.
type recordingStats struct {
	stats.Handler
	ends chan *stats.End
}

func newRecordingStats() *recordingStats {
	return &recordingStats{Handler: otelgrpc.NewServerHandler(), ends: make(chan *stats.End, 4)}
}

func (r *recordingStats) HandleRPC(ctx context.Context, rpcStats stats.RPCStats) {
	if end, ok := rpcStats.(*stats.End); ok {
		r.ends <- end
	}
	r.Handler.HandleRPC(ctx, rpcStats)
}

// The fail-safe. A verdict is only ever recorded by a handler that ran to completion, so an RPC that never reported one keeps
// whatever gRPC said about it, rather than being quietly recoloured by a wrapper that read silence as success. An RPC the auth
// interceptor refuses is the case that reaches this: its handler never runs.
func TestAnRPCThatReportedNoVerdictKeepsItsFailure(t *testing.T) {
	t.Parallel()
	inner := newRecordingStats()
	wrapped := handlerVerdictStats{Handler: inner}
	ctx := wrapped.TagRPC(context.Background(), &stats.RPCTagInfo{FullMethodName: "/rawtrail.control.v1.ControlChannel/Connect"})
	refused := status.Error(codes.Unauthenticated, "invalid host token")

	wrapped.HandleRPC(ctx, &stats.End{Error: refused})

	forwarded := <-inner.ends
	assert.Equal(t, refused, forwarded.Error, "nothing recorded a verdict for this RPC, so its failure stands")
}

// And the converse, which is the fix itself: an RPC whose handler reported success is forwarded with no error, so the span records
// the verdict rather than the transport's account of how the stream ended.
func TestAnRPCWhoseHandlerSucceededIsForwardedWithoutTheTransportError(t *testing.T) {
	t.Parallel()
	inner := newRecordingStats()
	wrapped := handlerVerdictStats{Handler: inner}
	ctx := wrapped.TagRPC(context.Background(), &stats.RPCTagInfo{FullMethodName: "/rawtrail.control.v1.ControlChannel/Connect"})
	recordSpanVerdict(ctx, nil)

	transportDied := errors.New("transport is closing")
	original := &stats.End{Error: transportDied}
	wrapped.HandleRPC(ctx, original)

	forwarded := <-inner.ends
	require.NoError(t, forwarded.Error)
	assert.Equal(t, transportDied, original.Error, "the event belongs to gRPC: it is copied, never mutated")
}

// TagRPC has to attach the verdict where the End event can find it: on the context gRPC threads through the RPC. Pinned directly
// because the wrapper silently degrades to a passthrough if it does not, which would look exactly like the bug it fixes.
func TestTheVerdictRidesTheContextTheEndEventCarries(t *testing.T) {
	t.Parallel()
	wrapped := handlerVerdictStats{Handler: otelgrpc.NewServerHandler()}

	ctx := wrapped.TagRPC(context.Background(), &stats.RPCTagInfo{FullMethodName: "/rawtrail.control.v1.ControlChannel/Connect"})

	verdict, ok := ctx.Value(spanVerdictKey{}).(*spanVerdict)
	require.True(t, ok, "without this the wrapper is an expensive passthrough")
	assert.False(t, verdict.succeeded.Load(), "a verdict starts unrecorded, so an RPC that never reports one keeps its failure")

	recordSpanVerdict(ctx, nil)
	assert.True(t, verdict.succeeded.Load())
	recordSpanVerdict(ctx, assert.AnError)
	assert.False(t, verdict.succeeded.Load(), "a handler that failed says so, even after an earlier success on the same RPC")
}

// A context with no verdict on it must not panic the handler: the value is absent for any RPC this gateway did not tag.
func TestRecordingAVerdictOnAnUntaggedContextIsInert(t *testing.T) {
	t.Parallel()
	assert.NotPanics(t, func() { recordSpanVerdict(context.Background(), nil) })
}
