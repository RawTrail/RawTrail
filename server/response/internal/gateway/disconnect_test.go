package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/rawtrail/rawtrail/internal/control"
	endpointapi "github.com/rawtrail/rawtrail/server/endpoint/api"
)

// These tests call the Connect handler directly rather than over a transport, because what they pin is the verdict it reaches: how
// the gateway itself classifies the end of a connection (issue #1124). The span that verdict produces is pinned separately, over a
// real transport, in spanverdict_test.go. Both halves are needed, and finding that out cost a round of QA: the handler's return is
// NOT what colours the span when a client disappears, because gRPC ends the RPC with the transport's own error regardless.

// fakeStream is a server stream whose RPC context, receive result and send outcome the test controls.
type fakeStream struct {
	grpc.ServerStream // unused on this path: Connect touches only Context, Recv and Send
	ctx               context.Context
	recvErr           chan error
	sendErr           error
	sent              chan *control.ServerFrame
}

// newFakeStream builds a stream carrying what the auth interceptor pins in production: the host id and the token it verified.
func newFakeStream(ctx context.Context, hostID, token string) *fakeStream {
	return &fakeStream{
		ctx:     context.WithValue(endpointapi.WithHostID(ctx, hostID), tokenKey, token),
		recvErr: make(chan error, 1),
		sent:    make(chan *control.ServerFrame, sendBuffer),
	}
}

func (s *fakeStream) Context() context.Context { return s.ctx }

// Send drops a frame nothing is reading rather than blocking the writer: heartbeats tick on their own here and are not what any of
// these tests assert.
func (s *fakeStream) Send(frame *control.ServerFrame) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	select {
	case s.sent <- frame:
	default:
	}
	return nil
}

// Recv blocks until the peer says something or the RPC ends, like the real one, so the handler's two ends race as they do in
// production: a client that goes away cancels the context AND fails the pending receive.
func (s *fakeStream) Recv() (*control.AgentFrame, error) {
	select {
	case err := <-s.recvErr:
		return nil, err
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

// connectedGateway starts Connect for one host against a fake stream and returns the gateway and a channel carrying what the handler
// returned, once the connection is registered.
func connectedGateway(t *testing.T, stream control.ControlChannel_ConnectServer) (*Gateway, <-chan error) {
	t.Helper()
	ver := newFakeVerifier()
	ver.add("tok-a", "host-a")
	g := New(Deps{Source: newFakeSource(), Verifier: ver})
	g.livenessInterval = 20 * time.Millisecond
	g.revocationInterval = 20 * time.Millisecond

	returned := make(chan error, 1)
	go func() { returned <- g.Connect(stream) }()
	require.Eventually(t, func() bool { return g.reg.get("host-a") != nil }, 2*time.Second, 10*time.Millisecond)
	return g, returned
}

// returnedFrom waits for the handler to return, bounded so a handler that never does fails the test instead of hanging the suite.
func returnedFrom(t *testing.T, returned <-chan error) error {
	t.Helper()
	select {
	case err := <-returned:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("the Connect handler never returned")
		return nil
	}
}

// requireTornDownBecause asserts the handler ended the RPC the way a server-initiated teardown must: retryably, so the client
// reconnects, and naming which teardown it was.
func requireTornDownBecause(t *testing.T, returned <-chan error, reason closeReason) {
	t.Helper()
	err := returnedFrom(t, returned)
	require.Error(t, err, "a client that is still attached has to be told to reconnect")
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), string(reason),
		"an operator asking why this host's channel dropped can only read the answer here")
}

// An agent restart, a closed lid, a dropped link: the ordinary end of a long-lived control connection, and the overwhelming majority
// of them. Recording each as a failure left the operation permanently error-coloured, so a real fault on the control path was
// indistinguishable from the background (issue #1124).
//
// spec:agent-control-channel/how-a-connection-ended-is-what-its-telemetry-reports/a-client-that-disconnects-ends-its-connection-cleanly
func TestConnectEndsCleanlyWhenTheClientWentAway(t *testing.T) {
	t.Parallel()
	ctx, clientGoesAway := context.WithCancel(t.Context())
	stream := newFakeStream(ctx, "host-a", "tok-a")
	g, returned := connectedGateway(t, stream)

	clientGoesAway()

	require.NoError(t, returnedFrom(t, returned), "an ordinary disconnect must not be recorded as a failed connection")
	assert.Nil(t, g.reg.get("host-a"), "and the connection is still deregistered")
}

// Each of these is a different operational story, and each is reached through the path that actually produces it rather than by
// setting the reason directly: what matters is that the connection carries the right one when the server ends it.
//
// spec:agent-control-channel/how-a-connection-ended-is-what-its-telemetry-reports/a-server-teardown-still-tells-the-client-to-reconnect
func TestConnectReportsWhyTheServerToreTheConnectionDown(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc string
		// prepare runs before the connection is made, for a teardown whose cause has to already be in place.
		prepare func(stream *fakeStream)
		// tearDown runs once the connection is registered.
		tearDown func(t *testing.T, g *Gateway, stream *fakeStream)
		want     closeReason
	}{
		{
			desc: "replaced by a reconnect from the same host",
			tearDown: func(t *testing.T, g *Gateway, _ *fakeStream) {
				t.Helper()
				second := newFakeStream(t.Context(), "host-a", "tok-a")
				go func() { _ = g.Connect(second) }()
			},
			want: reasonReplaced,
		},
		{
			desc:     "the host token is no longer valid",
			tearDown: func(_ *testing.T, g *Gateway, _ *fakeStream) { g.verifier.(*fakeVerifier).revoke("tok-a") },
			want:     reasonTokenInvalid,
		},
		{
			desc:     "the gateway is shutting down",
			tearDown: func(_ *testing.T, g *Gateway, _ *fakeStream) { g.Stop() },
			want:     reasonShuttingDown,
		},
		{
			// Set before connecting, so the first heartbeat the maintenance loop pushes fails the send and tears it down unprompted.
			// A send fails only on an ended stream, so the receive then fails too, once the teardown is recorded.
			desc:    "the outbound can no longer carry frames",
			prepare: func(stream *fakeStream) { stream.sendErr = errors.New("broken pipe") },
			tearDown: func(t *testing.T, g *Gateway, stream *fakeStream) {
				t.Helper()
				conn := g.reg.get("host-a")
				require.Eventually(t, func() bool { return conn.closedBecause() == reasonSendFailed }, 2*time.Second, 5*time.Millisecond)
				stream.recvErr <- errors.New("transport is closing")
			},
			want: reasonSendFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			stream := newFakeStream(t.Context(), "host-a", "tok-a")
			if tc.prepare != nil {
				tc.prepare(stream)
			}
			g, returned := connectedGateway(t, stream)

			tc.tearDown(t, g, stream)

			requireTornDownBecause(t, returned, tc.want)
		})
	}
}

// A stream that fails under a client that is still there is the case this whole change exists to keep visible. The failure is a
// gRPC status, as the real ones are: a frame this server cannot read is rejected with a code.
//
// spec:agent-control-channel/how-a-connection-ended-is-what-its-telemetry-reports/a-receive-failure-is-still-recorded-as-a-failure
func TestConnectReportsAReceiveFailureWhileTheClientIsAttached(t *testing.T) {
	t.Parallel()
	stream := newFakeStream(t.Context(), "host-a", "tok-a")
	_, returned := connectedGateway(t, stream)

	unreadable := status.Error(codes.Internal, "grpc: error unmarshalling request")
	stream.recvErr <- unreadable

	require.ErrorIs(t, returnedFrom(t, returned), unreadable)
}

// liveConn is a registered connection the server has not torn down.
func liveConn(t *testing.T) *conn {
	t.Helper()
	_, cancel := context.WithCancel(t.Context())
	return newConn("host-a", "tok-a", cancel)
}

// closedConn is a connection the server has torn down for the given reason, with no live stream behind it.
func closedConn(t *testing.T, reason closeReason) *conn {
	t.Helper()
	c := liveConn(t)
	c.close(reason)
	return c
}

// doneCtx is a context whose client has already gone.
func doneCtx(t *testing.T) context.Context {
	t.Helper()
	gone, cancel := context.WithCancel(t.Context())
	cancel()
	return gone
}

// requireUnavailableBecause asserts err is the retryable status a server teardown ends with, naming that teardown.
func requireUnavailableBecause(t *testing.T, err error, reason closeReason) {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), string(reason))
}

func TestEndAfterTeardown(t *testing.T) {
	t.Parallel()
	cases := []struct {
		desc string
		// clientGone makes the RPC's own context done, which is the definitive "there is nobody to return a status to".
		clientGone bool
		reason     closeReason
		wantReason closeReason // "" means the RPC must end cleanly
	}{
		{
			// Even though something also closed the connection: the client leaving is what ended it, and a teardown racing the
			// disconnect is a consequence of it, not a fault to report.
			desc:       "the client is already gone, so there is nobody to return a status to",
			clientGone: true,
			reason:     reasonSendFailed,
		},
		{desc: "the server replaced it", reason: reasonReplaced, wantReason: reasonReplaced},
		{desc: "the token is no longer valid", reason: reasonTokenInvalid, wantReason: reasonTokenInvalid},
		{desc: "the gateway is shutting down", reason: reasonShuttingDown, wantReason: reasonShuttingDown},
		{desc: "the outbound failed", reason: reasonSendFailed, wantReason: reasonSendFailed},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if tc.clientGone {
				ctx = doneCtx(t)
			}

			err := endAfterTeardown(ctx, closedConn(t, tc.reason))

			if tc.wantReason == "" {
				assert.NoError(t, err)
				return
			}
			requireUnavailableBecause(t, err, tc.wantReason)
		})
	}
}

func TestEndAfterReceive(t *testing.T) {
	t.Parallel()
	// A frame this server could not read: gRPC spells that as a status, because the server is rejecting it with a code.
	unreadable := status.Error(codes.Internal, "grpc: error unmarshalling request")
	// The transport ending under the stream: a bare error carrying no status, which is what grpc-go's ErrConnClosing is.
	transportDied := errors.New("transport is closing")
	oversized := status.Error(codes.ResourceExhausted, "grpc: received message larger than max")

	cases := []struct {
		desc       string
		clientGone bool
		// tornDownBecause is non-empty when the server had already torn the connection down when the receive loop ended.
		tornDownBecause closeReason
		recvErr         error
		wantErr         error       // the exact error the RPC must end with
		wantReason      closeReason // or the teardown it must report instead
	}{
		{
			desc:    "a frame this server could not read is the failure",
			recvErr: unreadable,
			wantErr: unreadable,
		},
		{
			// gRPC cancels the stream context moments after it writes a rejection, so a cancelled context cannot clear it (#1405).
			desc:       "the same failure with the context already cancelled is still the failure",
			clientGone: true,
			recvErr:    unreadable,
			wantErr:    unreadable,
		},
		{
			// net/http's HTTP/2 server reports a dying client as a status of Unavailable (#1329).
			desc:       "a client that went away, reported as a status, is the disconnect",
			clientGone: true,
			recvErr:    status.Error(codes.Unavailable, "client disconnected"),
		},
		{
			desc:    "and so is one reported before the context is cancelled",
			recvErr: status.Error(codes.Unavailable, "client disconnected"),
		},
		{
			// The case that needs the error's shape read: the transport died and gRPC has not cancelled the context yet, so the
			// context test alone would call this a fault. Measured on edr-dev, where it was one.
			desc:    "the transport ending under a live context is still a disconnect",
			recvErr: transportDied,
		},
		{
			// A client half-close reaches here as no error at all, and has always ended the RPC cleanly.
			desc: "an end of stream with no failure stays clean",
		},
		{
			// The race the two cases have to agree on: cancelling the connection does not unblock a pending Recv, so a receive can
			// end for its own reasons while a teardown is already recorded. The teardown is the more specific answer.
			desc:            "a receive ending after the server tore the connection down reports the teardown",
			tornDownBecause: reasonTokenInvalid,
			recvErr:         transportDied,
			wantReason:      reasonTokenInvalid,
		},
		{
			desc:            "and so does a clean end of stream landing at the same moment",
			tornDownBecause: reasonReplaced,
			wantReason:      reasonReplaced,
		},
		{
			// gRPC has already sent the rejection to the client, and a heartbeat on the rejected stream fails behind it (#1626).
			desc:            "a rejected frame outranks the outbound failure it caused",
			clientGone:      true,
			tornDownBecause: reasonSendFailed,
			recvErr:         oversized,
			wantErr:         oversized,
		},
		{
			desc:            "and a teardown the server recorded at the same moment",
			tornDownBecause: reasonTokenInvalid,
			recvErr:         unreadable,
			wantErr:         unreadable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if tc.clientGone {
				ctx = doneCtx(t)
			}
			c := liveConn(t)
			if tc.tornDownBecause != "" {
				c = closedConn(t, tc.tornDownBecause)
			}

			err := endAfterReceive(ctx, c, tc.recvErr)

			switch {
			case tc.wantReason != "":
				requireUnavailableBecause(t, err, tc.wantReason)
			case tc.wantErr != nil:
				require.ErrorIs(t, err, tc.wantErr)
			default:
				assert.NoError(t, err)
			}
		})
	}
}

// lateRecvStream holds the receive until released, then fails it as gRPC does when it ends a stream: the failure arrives after the
// stream context is already cancelled.
type lateRecvStream struct {
	*fakeStream
	release chan struct{}
	err     error
}

func (s *lateRecvStream) Recv() (*control.AgentFrame, error) {
	<-s.release
	return nil, s.err
}

// gRPC cancels the stream context moments after it rejects a frame, so Connect can see the cancellation before it sees the receive
// result that says why. The rejection must still end the RPC, and an Unavailable status, which is how net/http reports a client that
// went away, must still end it cleanly, whichever way the race falls.
//
// spec:agent-control-channel/a-status-failure-after-a-disconnect-ends-cleanly/a-rejected-frame-is-a-fault
// spec:agent-control-channel/a-status-failure-after-a-disconnect-ends-cleanly/an-unavailable-status-is-a-disconnect
func TestConnectWaitsForTheReceiveResultOnceTheStreamHasEnded(t *testing.T) {
	t.Parallel()
	unreadable := status.Error(codes.Internal, "grpc: failed to unmarshal the received message")

	for _, tc := range []struct {
		name    string
		recvErr error
		wantErr error
	}{
		{"a rejected frame", unreadable, unreadable},
		{"a client that went away", status.Error(codes.Unavailable, "client disconnected"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			stream := &lateRecvStream{fakeStream: newFakeStream(ctx, "host-a", "tok-a"), release: make(chan struct{}), err: tc.recvErr}
			_, returned := connectedGateway(t, stream)

			cancel() // the stream ends first
			// Connect has to take the connection-context branch before the result exists; nothing can be waited on to know it has.
			time.Sleep(100 * time.Millisecond)
			close(stream.release)

			err := returnedFrom(t, returned)
			if tc.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// rejectedMidSendStream holds the first send in flight until the client's frame is rejected, then fails it, as a send on a stream gRPC
// has already ended does. The rejection reaches the receive loop only after the failed send has torn the connection down.
type rejectedMidSendStream struct {
	*fakeStream
	sending    chan struct{} // closed once a send is in flight
	rejected   chan struct{} // closed when gRPC rejects the client's frame
	sendFailed chan struct{} // closed once the in-flight send has failed
	once       sync.Once
	// tornDown reports whether the failed send's teardown is recorded. Set before rejected is closed, so Recv reads it safely.
	tornDown func() bool
}

func (s *rejectedMidSendStream) Send(*control.ServerFrame) error {
	s.once.Do(func() { close(s.sending) })
	<-s.rejected
	defer func() {
		select {
		case <-s.sendFailed:
		default:
			close(s.sendFailed)
		}
	}()
	return errors.New("transport: the stream is done")
}

func (s *rejectedMidSendStream) Recv() (*control.AgentFrame, error) {
	<-s.sendFailed
	// The rejection must not exist until the failed send's teardown is recorded. Unrecorded by the deadline, the receive fails with
	// something other than a rejection, so the test fails rather than passing on the wrong order.
	deadline := time.Now().Add(2 * time.Second)
	for !s.tornDown() {
		if time.Now().After(deadline) {
			return nil, errors.New("the failed send never recorded its teardown")
		}
		time.Sleep(time.Millisecond)
	}
	// Connect also has to have taken the connection-context branch, which nothing observable marks.
	time.Sleep(100 * time.Millisecond)
	return nil, status.Error(codes.ResourceExhausted, "grpc: received message larger than max")
}

// gRPC writes an oversized frame's rejection to the client itself, and a heartbeat already being sent then fails on the ended stream,
// which tears the connection down as an outbound failure. The RPC must still end with the rejection the client received, or the span
// names a status nobody was sent (issue #1626).
//
// spec:agent-control-channel/a-rejected-frame-outranks-the-failures-it-causes/a-heartbeat-in-flight-does-not-replace-the-rejection
func TestARejectedFrameOutranksTheHeartbeatItBreaks(t *testing.T) {
	t.Parallel()
	stream := &rejectedMidSendStream{
		fakeStream: newFakeStream(t.Context(), "host-a", "tok-a"),
		sending:    make(chan struct{}), rejected: make(chan struct{}), sendFailed: make(chan struct{}),
	}
	g, returned := connectedGateway(t, stream)
	conn := g.reg.get("host-a")
	require.NotNil(t, conn)
	stream.tornDown = func() bool { return conn.closedBecause() == reasonSendFailed }

	<-stream.sending // a heartbeat is in flight
	close(stream.rejected)

	err := returnedFrom(t, returned)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err), "the RPC ends with the status the client was sent")
}

// Connect waits for the receive result whenever the stream is over under it. A teardown recorded in the same instant gRPC rejected a
// frame and cancelled the RPC context is one of those: returning on it would end the RPC cleanly and drop the rejection the client
// was sent. Only a teardown under a live RPC context, which the client can still receive, is reported without waiting.
//
// spec:agent-control-channel/a-rejected-frame-outranks-the-failures-it-causes/a-heartbeat-in-flight-does-not-replace-the-rejection
func TestStreamEnded(t *testing.T) {
	t.Parallel()
	cases := []struct {
		desc   string
		rpcEnd bool
		reason closeReason
		want   bool
	}{
		{"the client went away", true, "", true},
		{"a revocation recorded as the RPC ended", true, reasonTokenInvalid, true},
		{"a replacement recorded as the RPC ended", true, reasonReplaced, true},
		{"the outbound failed under a live RPC", false, reasonSendFailed, true},
		{"a revocation the client can still receive", false, reasonTokenInvalid, false},
		{"a shutdown the client can still receive", false, reasonShuttingDown, false},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if tc.rpcEnd {
				ctx = doneCtx(t)
			}
			assert.Equal(t, tc.want, streamEnded(ctx, tc.reason))
		})
	}
}

// Once the connection's context is done, a receive result that is already there decides the verdict, whatever teardown was recorded
// beside it. select picks between ready cases at random, so without this a revocation recorded as gRPC rejected a frame named the
// revocation half the time, a status the client was never sent.
//
// spec:agent-control-channel/a-rejected-frame-outranks-the-failures-it-causes/a-revocation-recorded-with-the-rejection-does-not-replace-it
func TestEndAfterConnClosedReadsAReceiveResultAlreadyThere(t *testing.T) {
	t.Parallel()
	oversized := status.Error(codes.ResourceExhausted, "grpc: received message larger than max")
	cases := []struct {
		desc       string
		received   bool  // the receive loop's result is already on the channel
		recvErr    error // what it carries
		wantErr    error
		wantReason closeReason
	}{
		{desc: "a rejection already received outranks a revocation", received: true, recvErr: oversized, wantErr: oversized},
		{desc: "a disconnect already received still reports the revocation", received: true, recvErr: errors.New("transport is closing"),
			wantReason: reasonTokenInvalid},
		{desc: "with nothing received a revocation the client can receive does not wait", wantReason: reasonTokenInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			recvErr := make(chan error, 1)
			if tc.received {
				recvErr <- tc.recvErr
			}
			returned := make(chan error, 1)
			go func() { returned <- endAfterConnClosed(t.Context(), closedConn(t, reasonTokenInvalid), recvErr) }()

			var err error
			select {
			case err = <-returned:
			case <-time.After(recvSettleTimeout / 2):
				require.FailNow(t, "waited on the receive loop for a teardown the client can still receive")
			}
			if tc.wantReason != "" {
				requireUnavailableBecause(t, err, tc.wantReason)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
