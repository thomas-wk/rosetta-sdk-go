package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coinbase/rosetta-sdk-go/types"
)

func TestApiTimeouts(t *testing.T) {
	ctx, testDone := context.WithCancel(context.Background())
	defer testDone()
	durationHeaderKey := "X-Sleep-Duration"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sleepDur, err := strconv.Atoi(r.Header.Get(durationHeaderKey))
		if err != nil {
			t.Errorf("Atoi(Header[%s]:%s) got err: %v", durationHeaderKey, r.Header.Get(durationHeaderKey), err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case <-time.After(time.Duration(sleepDur)):
			w.WriteHeader(http.StatusTeapot)
		case <-ctx.Done():
			w.WriteHeader(http.StatusServiceUnavailable)
		}

	}))
	defer s.Close()

	tests := []struct {
		name           string
		serverDelay    time.Duration
		cfgTO          time.Duration
		ctxTO          time.Duration
		responseWithin time.Duration
		cxlApi         bool
		errSubstr      string
	}{
		{name: "ctx timeout shortest", serverDelay: 10 * time.Second, cfgTO: 10 * time.Second, ctxTO: 5 * time.Millisecond, responseWithin: 100 * time.Millisecond, errSubstr: "context deadline"},
		{name: "cfg timeout shortest", serverDelay: 10 * time.Second, cfgTO: 10 * time.Millisecond, ctxTO: 5 * time.Second, responseWithin: 100 * time.Millisecond, errSubstr: "context deadline"},
		{name: "no ctx timeout uses cfg", serverDelay: 10 * time.Second, cfgTO: 15 * time.Millisecond, responseWithin: 100 * time.Millisecond, errSubstr: "context deadline"},
		{name: "cxl ends api call", serverDelay: 10 * time.Second, cfgTO: 10 * time.Second, cxlApi: true, responseWithin: 50 * time.Millisecond, errSubstr: "context cancel"},
		{name: "non-timeout case", serverDelay: 1 * time.Millisecond, cfgTO: 10 * time.Second, responseWithin: 20 * time.Millisecond, errSubstr: strconv.Itoa(http.StatusTeapot)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := NewConfiguration(s.URL, "testing", nil)
			cfg.AddDefaultHeader(durationHeaderKey, strconv.Itoa(int(tc.serverDelay)))
			if tc.cfgTO != 0 {
				cfg.NetworkRoundTripTimeout = tc.cfgTO
			}

			c := NewAPIClient(cfg)
			ctx, cxl := context.WithCancel(ctx)
			defer cxl()
			if tc.ctxTO != 0 {
				ctx, cxl = context.WithTimeout(ctx, tc.ctxTO)
				defer cxl()
			}
			if tc.cxlApi {
				go func() {
					time.Sleep(10 * time.Millisecond)
					cxl()
				}()
			}
			start := time.Now()
			_, _, err := c.NetworkAPI.NetworkList(ctx, &types.MetadataRequest{})
			callDuration := time.Since(start)
			if err == nil {
				t.Errorf("Call should always fail!")
			}
			if !strings.Contains(err.Error(), tc.errSubstr) {
				t.Errorf("Got err: %v - wanted message containing: %s", err, tc.errSubstr)
			}
			if callDuration > tc.responseWithin {
				t.Errorf("Call completed in %d ; wanted to finish within: %d", callDuration, tc.responseWithin)
			}

		})
	}
	// end any threads sleeping in the test server
	testDone()
}
