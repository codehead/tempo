package azure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/grafana/tempo/tempodb/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHedge(t *testing.T) {
	tests := []struct {
		name                   string
		returnIn               time.Duration
		hedgeAt                time.Duration
		expectedHedgedRequests int32
	}{
		{
			name:                   "hedge disabled",
			expectedHedgedRequests: 1,
		},
		{
			name:                   "hedge enabled doesn't hit",
			hedgeAt:                time.Hour,
			expectedHedgedRequests: 1,
		},
		{
			name:                   "hedge enabled and hits",
			hedgeAt:                time.Millisecond,
			returnIn:               100 * time.Millisecond,
			expectedHedgedRequests: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count := int32(0)
			server := fakeServer(t, tc.returnIn, &count)

			r, w, _, err := New(&Config{
				MaxBuffers:      3,
				BufferSize:      1000,
				ContainerName:   "blerg",
				Endpoint:        server.URL[7:], // [7:] -> strip http://,
				HedgeRequestsAt: tc.hedgeAt,
			})
			require.NoError(t, err)

			ctx := context.Background()

			// the first call on each client initiates an extra http request
			// clearing that here
			_, _ = r.Read(ctx, "object", uuid.New(), "tenant")
			time.Sleep(tc.returnIn)
			atomic.StoreInt32(&count, 0)

			// calls that should hedge
			_, _ = r.Read(ctx, "object", uuid.New(), "tenant")
			time.Sleep(tc.returnIn)
			assert.Equal(t, tc.expectedHedgedRequests*2, atomic.LoadInt32(&count)) // *2 b/c reads execute a HEAD and GET
			atomic.StoreInt32(&count, 0)

			// this panics with the garbage test setup. todo: make it not panic
			// _ = r.ReadRange(ctx, "object", uuid.New(), "tenant", 10, make([]byte, 100))
			// time.Sleep(tc.returnIn)
			// assert.Equal(t, tc.expectedHedgedRequests, atomic.LoadInt32(&count))
			// atomic.StoreInt32(&count, 0)

			_, _ = r.BlockMeta(ctx, uuid.New(), "tenant") // *2 b/c reads execute a HEAD and GET
			time.Sleep(tc.returnIn)
			assert.Equal(t, tc.expectedHedgedRequests*2, atomic.LoadInt32(&count))
			atomic.StoreInt32(&count, 0)

			// calls that should not hedge
			_, _ = r.Tenants(ctx)
			assert.Equal(t, int32(1), atomic.LoadInt32(&count))
			atomic.StoreInt32(&count, 0)

			_, _ = r.Blocks(ctx, "tenant")
			assert.Equal(t, int32(1), atomic.LoadInt32(&count))
			atomic.StoreInt32(&count, 0)

			_ = w.Write(ctx, "object", uuid.New(), "tenant", make([]byte, 10))
			assert.Equal(t, int32(1), atomic.LoadInt32(&count))
			atomic.StoreInt32(&count, 0)

			_ = w.WriteBlockMeta(ctx, &backend.BlockMeta{})
			assert.Equal(t, int32(1), atomic.LoadInt32(&count))
			atomic.StoreInt32(&count, 0)
		})
	}
}

func fakeServer(t *testing.T, returnIn time.Duration, counter *int32) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(returnIn)

		atomic.AddInt32(counter, 1)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	return server
}
