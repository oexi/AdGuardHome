package upstreamsub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/agh"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/AdguardTeam/golibs/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRegistrar is an [aghhttp.Registrar] for tests.
type testRegistrar struct {
	mux *http.ServeMux
}

// Register implements the [aghhttp.Registrar] interface for *testRegistrar.
func (r *testRegistrar) Register(method, path string, h http.HandlerFunc) {
	r.mux.HandleFunc(method+" "+path, h)
}

// newTestManager returns a new manager and the HTTP handler of its API.
func newTestManager(t *testing.T, conf *Config) (m *Manager, h http.Handler, changes *atomic.Int32) {
	t.Helper()

	changes = &atomic.Int32{}
	reg := &testRegistrar{mux: http.NewServeMux()}
	m, err := New(testutil.ContextWithTimeout(t, testTimeout), &ManagerConfig{
		Logger:       slogutil.NewDiscardLogger(),
		Config:       conf,
		HTTPClient:   http.DefaultClient,
		ConfModifier: agh.EmptyConfigModifier{},
		HTTPReg:      reg,
		OnChange:     func(_ context.Context) { changes.Add(1) },
		DataDir:      t.TempDir(),
	})
	require.NoError(t, err)
	testutil.CleanupAndRequireSuccess(t, func() (err error) { m.Close(); return nil })

	return m, reg.mux, changes
}

// testTimeout is the common timeout for tests.
const testTimeout = 10 * time.Second

// do performs the API request and returns the response recorder.
func do(t *testing.T, h http.Handler, method, path string, body any) (rw *httptest.ResponseRecorder) {
	t.Helper()

	var b []byte
	if body != nil {
		var err error
		b, err = json.Marshal(body)
		require.NoError(t, err)
	}

	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	rw = httptest.NewRecorder()
	h.ServeHTTP(rw, r)

	return rw
}

func TestManager(t *testing.T) {
	t.Parallel()

	contents := &atomic.Value{}
	contents.Store("server=/example.org/114.114.114.114\nserver=/example.net/114.114.114.114\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(contents.Load().(string)))
	}))
	t.Cleanup(srv.Close)

	m, h, changes := newTestManager(t, &Config{
		Enabled:             true,
		UpdateIntervalHours: 24,
	})

	ctx := testutil.ContextWithTimeout(t, testTimeout)
	manual := []string{"1.1.1.1", "[/example.net/]8.8.8.8"}

	rw := do(t, h, http.MethodPost, "/control/upstream_subscriptions/add", map[string]any{
		"name": "test",
		"url":  srv.URL,
	})
	require.Equal(t, http.StatusOK, rw.Code, rw.Body.String())
	assert.Equal(t, int32(1), changes.Load())

	// The manually configured domain takes precedence.
	assert.Equal(t, []string{
		"1.1.1.1",
		"[/example.net/]8.8.8.8",
		"[/example.org/]114.114.114.114",
	}, m.ExtendUpstreams(ctx, manual))

	rw = do(t, h, http.MethodPost, "/control/upstream_subscriptions/add", map[string]any{
		"url": srv.URL,
	})
	assert.Equal(t, http.StatusBadRequest, rw.Code)

	rw = do(t, h, http.MethodPost, "/control/upstream_subscriptions/set", map[string]any{
		"id": 1,
		"data": map[string]any{
			"name":      "test",
			"url":       srv.URL,
			"upstreams": []string{"223.5.5.5 119.29.29.29"},
		},
	})
	require.Equal(t, http.StatusOK, rw.Code, rw.Body.String())
	assert.Equal(t, int32(2), changes.Load())
	assert.Equal(t, []string{
		"1.1.1.1",
		"[/example.net/]8.8.8.8",
		"[/example.org/]223.5.5.5 119.29.29.29",
	}, m.ExtendUpstreams(ctx, manual))

	contents.Store("example.com\nexample.org\n")
	rw = do(t, h, http.MethodPost, "/control/upstream_subscriptions/refresh", nil)
	require.Equal(t, http.StatusOK, rw.Code, rw.Body.String())
	assert.JSONEq(t, `{"updated":1,"errors":[]}`, rw.Body.String())
	assert.Equal(t, int32(3), changes.Load())
	assert.Equal(t, []string{
		"1.1.1.1",
		"[/example.net/]8.8.8.8",
		"[/example.com/example.org/]223.5.5.5 119.29.29.29",
	}, m.ExtendUpstreams(ctx, manual))

	// Nothing changed.
	rw = do(t, h, http.MethodPost, "/control/upstream_subscriptions/refresh", map[string]any{
		"id": 1,
	})
	require.Equal(t, http.StatusOK, rw.Code, rw.Body.String())
	assert.JSONEq(t, `{"updated":0,"errors":[]}`, rw.Body.String())
	assert.Equal(t, int32(3), changes.Load())

	rw = do(t, h, http.MethodGet, "/control/upstream_subscriptions/status", nil)
	require.Equal(t, http.StatusOK, rw.Code)

	status := &statusJSON{}
	require.NoError(t, json.Unmarshal(rw.Body.Bytes(), status))
	require.Len(t, status.Subscriptions, 1)

	sub := status.Subscriptions[0]
	assert.Equal(t, 2, sub.RulesCount)
	assert.Equal(t, []string{"223.5.5.5", "119.29.29.29"}, sub.Upstreams)
	assert.NotEmpty(t, sub.LastUpdated)

	// Disable all.
	rw = do(t, h, http.MethodPost, "/control/upstream_subscriptions/config", map[string]any{
		"enabled": false,
	})
	require.Equal(t, http.StatusOK, rw.Code, rw.Body.String())
	assert.Equal(t, int32(4), changes.Load())
	assert.Equal(t, manual, m.ExtendUpstreams(ctx, manual))

	rw = do(t, h, http.MethodPost, "/control/upstream_subscriptions/remove", map[string]any{
		"id": 1,
	})
	require.Equal(t, http.StatusOK, rw.Code, rw.Body.String())
	assert.NoFileExists(t, m.path(1))

	conf := &Config{}
	m.WriteDiskConfig(conf)
	assert.Empty(t, conf.Subscriptions)
	assert.False(t, conf.Enabled)
}

func TestManager_loadFromDisk(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	require.NoError(t, os.MkdirAll(dataDir+"/"+dirName, 0o700))

	err := os.WriteFile(dataDir+"/"+dirName+"/5.txt", []byte("example.org\n"), 0o600)
	require.NoError(t, err)

	m, err := New(testutil.ContextWithTimeout(t, testTimeout), &ManagerConfig{
		Logger: slogutil.NewDiscardLogger(),
		Config: &Config{
			Enabled: true,
			Subscriptions: []*Subscription{{
				URL:       "https://example.invalid/list.txt",
				Upstreams: []string{"9.9.9.9"},
				ID:        5,
				Enabled:   true,
			}, {
				URL:     "https://example.invalid/disabled.txt",
				ID:      6,
				Enabled: false,
			}},
		},
		HTTPClient:   http.DefaultClient,
		ConfModifier: agh.EmptyConfigModifier{},
		HTTPReg:      &testRegistrar{mux: http.NewServeMux()},
		OnChange:     func(_ context.Context) {},
		DataDir:      dataDir,
	})
	require.NoError(t, err)

	ctx := testutil.ContextWithTimeout(t, testTimeout)
	assert.Equal(t, []string{"[/example.org/]9.9.9.9"}, m.ExtendUpstreams(ctx, nil))
}
