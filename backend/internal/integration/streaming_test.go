package integration

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Notifications travel on a long-lived SSE stream that the client opens
// and leaves open. If anything in the chain buffered that response, a
// notification would reach the client only when the stream closed —
// which is to say when the session ended, long after it mattered.
//
// This is the property the whole gateway rests on, so it is asserted
// against the real router, the real gateway and a real upstream server
// rather than a stand-in for any of them.
func TestANotificationArrivesWhileTheStreamIsOpen(t *testing.T) {
	stack := start(t, map[string]string{"files": "full"})

	changed := make(chan time.Time, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "stream-test", Version: "1.0"},
		&mcp.ClientOptions{
			ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
				select {
				case changed <- time.Now():
				default:
				}
			},
		})
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: stack.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	// Anything left over from establishing the session is not what is
	// being measured.
	for len(changed) > 0 {
		<-changed
	}

	// grow adds a tool upstream, which the upstream server announces,
	// which the gateway republishes to its own clients.
	started := time.Now()
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "files_grow", Arguments: map[string]any{},
	}); err != nil {
		t.Fatalf("call the tool that changes the upstream list: %v", err)
	}

	select {
	case at := <-changed:
		took := at.Sub(started)
		t.Logf("the notification arrived %v after the change, with the session still open", took)

		// The session is deliberately still open. Had the response been
		// buffered, nothing would have arrived at all.
		if took > 10*time.Second {
			t.Errorf("the notification took %v; it was held rather than streamed", took)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("no notification arrived while the stream was open; something buffered it")
	}

	// The new tool really is on offer, so the notification was not empty.
	eventually(t, "the new tool to be listed", func() bool {
		return contains(toolNames(t, session), "files_grown")
	})
}

// Several clients share one gateway, and a notification has to reach
// every one of them: a stream that only the first client receives would
// leave the others silently stale.
func TestEveryConnectedClientIsNotified(t *testing.T) {
	stack := start(t, map[string]string{"files": "full"})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const clients = 3
	notified := make([]chan struct{}, clients)
	sessions := make([]*mcp.ClientSession, clients)

	for i := range clients {
		notified[i] = make(chan struct{}, 4)
		signal := notified[i]

		client := mcp.NewClient(&mcp.Implementation{Name: "watcher", Version: "1.0"},
			&mcp.ClientOptions{
				ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
					select {
					case signal <- struct{}{}:
					default:
					}
				},
			})
		session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: stack.URL}, nil)
		if err != nil {
			t.Fatalf("connect client %d: %v", i, err)
		}
		defer session.Close()
		sessions[i] = session
	}

	for _, signal := range notified {
		for len(signal) > 0 {
			<-signal
		}
	}

	if _, err := sessions[0].CallTool(ctx, &mcp.CallToolParams{
		Name: "files_grow", Arguments: map[string]any{},
	}); err != nil {
		t.Fatalf("call the tool that changes the upstream list: %v", err)
	}

	for i, signal := range notified {
		select {
		case <-signal:
		case <-time.After(30 * time.Second):
			t.Errorf("client %d was never told the tool list changed", i)
		}
	}
}

// A session ends with a DELETE on the endpoint. The router has to pass
// that through like any other method, or a client could never release
// its session and the gateway would hold them until they timed out.
func TestASessionCanBeEnded(t *testing.T) {
	stack := start(t, map[string]string{"files": "full"})
	session := stack.connect(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatalf("the session does not work to begin with: %v", err)
	}

	if err := session.Close(); err != nil {
		t.Errorf("ending the session: %v", err)
	}

	eventually(t, "the session to be released", func() bool {
		for _, info := range stack.Gateway.Sessions() {
			if info.ID == session.ID() {
				return false
			}
		}
		return true
	})
}
