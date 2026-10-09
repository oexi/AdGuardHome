package upstreamsub

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
)

// registerHandlers registers the HTTP API handlers.
func (m *Manager) registerHandlers(reg aghhttp.Registrar) {
	reg.Register(http.MethodGet, "/control/upstream_subscriptions/status", m.handleStatus)
	reg.Register(http.MethodPost, "/control/upstream_subscriptions/config", m.handleConfig)
	reg.Register(http.MethodPost, "/control/upstream_subscriptions/add", m.handleAdd)
	reg.Register(http.MethodPost, "/control/upstream_subscriptions/set", m.handleSet)
	reg.Register(http.MethodPost, "/control/upstream_subscriptions/remove", m.handleRemove)
	reg.Register(http.MethodPost, "/control/upstream_subscriptions/refresh", m.handleRefresh)
}

// subscriptionJSON is the JSON representation of a subscription.
type subscriptionJSON struct {
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	LastUpdated string   `json:"last_updated,omitempty"`
	LastError   string   `json:"last_error,omitempty"`
	Upstreams   []string `json:"upstreams"`
	ID          uint64   `json:"id"`
	RulesCount  int      `json:"rules_count"`
	Invalid     int      `json:"invalid_count"`
	Enabled     bool     `json:"enabled"`
}

// statusJSON is the response of the status handler.
type statusJSON struct {
	Subscriptions []*subscriptionJSON `json:"subscriptions"`
	Interval      uint32              `json:"interval"`
	Enabled       bool                `json:"enabled"`
}

// handleStatus is the handler for GET /control/upstream_subscriptions/status.
func (m *Manager) handleStatus(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	resp := &statusJSON{
		Subscriptions: make([]*subscriptionJSON, 0, len(m.conf.Subscriptions)),
		Interval:      m.conf.UpdateIntervalHours,
		Enabled:       m.conf.Enabled,
	}

	for _, sub := range m.conf.Subscriptions {
		sj := &subscriptionJSON{
			Name:      sub.Name,
			URL:       sub.URL,
			Upstreams: slices.Clone(sub.Upstreams),
			ID:        sub.ID,
			Enabled:   sub.Enabled,
		}

		if sj.Upstreams == nil {
			sj.Upstreams = []string{}
		}

		if !sub.LastUpdated.IsZero() {
			sj.LastUpdated = sub.LastUpdated.Format(time.RFC3339)
		}

		if st := m.states[sub.ID]; st != nil {
			if st.res != nil {
				sj.RulesCount = st.res.rulesCount
				sj.Invalid = st.res.invalidCount
			}

			if st.lastErr != nil {
				sj.LastError = st.lastErr.Error()
			}
		}

		resp.Subscriptions = append(resp.Subscriptions, sj)
	}
	m.mu.RUnlock()

	aghhttp.WriteJSONResponseOK(r.Context(), m.logger, w, r, resp)
}

// configJSON is the request body of the config handler.
type configJSON struct {
	Interval *uint32 `json:"interval"`
	Enabled  *bool   `json:"enabled"`
}

// maxIntervalHours is the maximum update interval, a year.
const maxIntervalHours = 365 * 24

// handleConfig is the handler for POST /control/upstream_subscriptions/config.
func (m *Manager) handleConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	req := &configJSON{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusBadRequest, "decoding: %s", err)

		return
	}

	if req.Interval != nil && *req.Interval > maxIntervalHours {
		aghhttp.ErrorAndLog(
			ctx,
			m.logger,
			r,
			w,
			http.StatusBadRequest,
			"interval: must be at most %d",
			maxIntervalHours,
		)

		return
	}

	m.mu.Lock()
	enabledChanged := req.Enabled != nil && *req.Enabled != m.conf.Enabled
	if req.Enabled != nil {
		m.conf.Enabled = *req.Enabled
	}

	if req.Interval != nil {
		m.conf.UpdateIntervalHours = *req.Interval
	}
	m.mu.Unlock()

	m.confModifier.Apply(ctx)
	if enabledChanged {
		m.onChange(ctx)
	}

	aghhttp.OK(ctx, m.logger, w)
}

// subscriptionDataJSON is the user-editable data of a subscription.
type subscriptionDataJSON struct {
	Enabled   *bool    `json:"enabled"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Upstreams []string `json:"upstreams"`
}

// validate validates and normalizes d.  It returns the normalized upstreams.
func (d *subscriptionDataJSON) validate() (ups []string, err error) {
	d.Name = strings.TrimSpace(d.Name)
	d.URL = strings.TrimSpace(d.URL)

	err = validateURL(d.URL)
	if err != nil {
		// Don't wrap the error since it's informative enough as is.
		return nil, err
	}

	if d.Name == "" {
		d.Name = d.URL
	}

	return normalizeUpstreams(d.Upstreams)
}

// fetched is the downloaded and parsed contents of a subscription.
type fetched struct {
	res  *parseResult
	data []byte
}

// fetch downloads and parses the subscription at u with the given override.
// It returns an error if the subscription contains no valid rules.
func (m *Manager) fetch(ctx context.Context, u string, override []string) (f *fetched, err error) {
	data, err := m.download(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("downloading: %w", err)
	}

	res := parse(data, override)
	if res.rulesCount == 0 {
		return nil, fmt.Errorf(
			"no valid rules found, %d invalid lines; set upstreams to use a domain list",
			res.invalidCount,
		)
	}

	return &fetched{res: res, data: data}, nil
}

// handleAdd is the handler for POST /control/upstream_subscriptions/add.
func (m *Manager) handleAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &subscriptionDataJSON{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding: %s", err)

		return
	}

	ups, err := req.validate()
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	m.updateMu.Lock()
	defer m.updateMu.Unlock()

	if m.hasURL(req.URL, 0) {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "url %q already exists", req.URL)

		return
	}

	f, err := m.fetch(ctx, req.URL, ups)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	sub := &Subscription{
		LastUpdated: time.Now(),
		Name:        req.Name,
		URL:         req.URL,
		Upstreams:   ups,
		Enabled:     req.Enabled == nil || *req.Enabled,
	}

	m.mu.Lock()
	sub.ID = m.nextID()
	err = m.save(sub.ID, f.data)
	if err == nil {
		m.conf.Subscriptions = append(m.conf.Subscriptions, sub)
		m.states[sub.ID] = &subState{res: f.res, checksum: sha256.Sum256(f.data)}
	}
	m.mu.Unlock()

	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusInternalServerError, "saving: %s", err)

		return
	}

	l.InfoContext(ctx, "added subscription", "id", sub.ID, "url", sub.URL)

	m.confModifier.Apply(ctx)
	if sub.Enabled && m.isEnabled() {
		m.onChange(ctx)
	}

	aghhttp.OK(ctx, l, w)
}

// hasURL returns true if there is a subscription with URL u and an identifier
// other than exceptID.
func (m *Manager) hasURL(u string, exceptID uint64) (ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return slices.ContainsFunc(m.conf.Subscriptions, func(sub *Subscription) (found bool) {
		return sub.ID != exceptID && sub.URL == u
	})
}

// nextID returns an unused subscription identifier.  m.mu is expected to be
// locked.
func (m *Manager) nextID() (id uint64) {
	for _, sub := range m.conf.Subscriptions {
		id = max(id, sub.ID)
	}

	return id + 1
}

// setJSON is the request body of the set handler.
type setJSON struct {
	Data *subscriptionDataJSON `json:"data"`
	ID   uint64                `json:"id"`
}

// handleSet is the handler for POST /control/upstream_subscriptions/set.
func (m *Manager) handleSet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &setJSON{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding: %s", err)

		return
	} else if req.Data == nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "data: %s", errors.ErrNoValue)

		return
	}

	ups, err := req.Data.validate()
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "%s", err)

		return
	}

	m.updateMu.Lock()
	defer m.updateMu.Unlock()

	code, err := m.set(ctx, req.ID, req.Data, ups)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, code, "%s", err)

		return
	}

	aghhttp.OK(ctx, l, w)
}

// set applies the changes to the subscription with the given id.  code is the
// HTTP status code to respond with in case of an error.  m.updateMu is
// expected to be locked.
func (m *Manager) set(
	ctx context.Context,
	id uint64,
	d *subscriptionDataJSON,
	ups []string,
) (code int, err error) {
	m.mu.RLock()
	cur := m.find(id)
	if cur != nil {
		cur = cur.clone()
	}
	m.mu.RUnlock()

	if cur == nil {
		return http.StatusBadRequest, fmt.Errorf("subscription with id %d not found", id)
	}

	if m.hasURL(d.URL, id) {
		return http.StatusBadRequest, fmt.Errorf("url %q already exists", d.URL)
	}

	enabled := cur.Enabled
	if d.Enabled != nil {
		enabled = *d.Enabled
	}

	var f *fetched
	switch {
	case d.URL != cur.URL:
		f, err = m.fetch(ctx, d.URL, ups)
		if err != nil {
			return http.StatusBadRequest, err
		}
	case !slices.Equal(ups, cur.Upstreams):
		f, err = m.reparse(ctx, id, d.URL, ups)
		if err != nil {
			return http.StatusBadRequest, err
		}
	default:
		// Only the name or the enabled state changed.
	}

	m.mu.Lock()
	sub := m.find(id)
	if sub != nil {
		if f != nil {
			err = m.save(id, f.data)
		}

		if err == nil {
			upstreamsChanged := f != nil || sub.Enabled != enabled
			sub.Name, sub.URL, sub.Upstreams, sub.Enabled = d.Name, d.URL, ups, enabled
			if f != nil {
				sub.LastUpdated = time.Now()
				m.states[id] = &subState{res: f.res, checksum: sha256.Sum256(f.data)}
			}

			defer m.afterSet(ctx, upstreamsChanged)
		}
	}
	m.mu.Unlock()

	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("saving: %w", err)
	}

	return http.StatusOK, nil
}

// afterSet persists the configuration and applies the changed upstreams, if
// needed.  It must be called without m.mu locked.
func (m *Manager) afterSet(ctx context.Context, upstreamsChanged bool) {
	m.confModifier.Apply(ctx)
	if upstreamsChanged && m.isEnabled() {
		m.onChange(ctx)
	}
}

// reparse parses the cached contents of the subscription with the given id
// with the new override.  If there are no cached contents, it downloads them
// from u.
func (m *Manager) reparse(
	ctx context.Context,
	id uint64,
	u string,
	override []string,
) (f *fetched, err error) {
	data, err := os.ReadFile(m.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return m.fetch(ctx, u, override)
	} else if err != nil {
		return nil, fmt.Errorf("reading cached contents: %w", err)
	}

	res := parse(data, override)
	if res.rulesCount == 0 {
		return nil, fmt.Errorf("no valid rules found, %d invalid lines", res.invalidCount)
	}

	return &fetched{res: res, data: data}, nil
}

// removeJSON is the request body of the remove handler.
type removeJSON struct {
	ID uint64 `json:"id"`
}

// handleRemove is the handler for POST /control/upstream_subscriptions/remove.
func (m *Manager) handleRemove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := m.logger

	req := &removeJSON{}
	err := json.NewDecoder(r.Body).Decode(req)
	if err != nil {
		aghhttp.ErrorAndLog(ctx, l, r, w, http.StatusBadRequest, "decoding: %s", err)

		return
	}

	m.updateMu.Lock()
	defer m.updateMu.Unlock()

	m.mu.Lock()
	idx := slices.IndexFunc(m.conf.Subscriptions, func(sub *Subscription) (ok bool) {
		return sub.ID == req.ID
	})

	var wasEnabled bool
	if idx >= 0 {
		wasEnabled = m.conf.Subscriptions[idx].Enabled
		m.conf.Subscriptions = slices.Delete(m.conf.Subscriptions, idx, idx+1)
		delete(m.states, req.ID)
	}
	m.mu.Unlock()

	if idx < 0 {
		aghhttp.ErrorAndLog(
			ctx,
			l,
			r,
			w,
			http.StatusBadRequest,
			"subscription with id %d not found",
			req.ID,
		)

		return
	}

	err = os.Remove(m.path(req.ID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		l.ErrorContext(ctx, "removing subscription file", "id", req.ID, slogutil.KeyError, err)
	}

	l.InfoContext(ctx, "removed subscription", "id", req.ID)

	m.confModifier.Apply(ctx)
	if wasEnabled && m.isEnabled() {
		m.onChange(ctx)
	}

	aghhttp.OK(ctx, l, w)
}

// refreshJSON is the request body of the refresh handler.
type refreshJSON struct {
	// ID, if not zero, is the identifier of the only subscription to update.
	ID uint64 `json:"id"`
}

// refreshRespJSON is the response of the refresh handler.
type refreshRespJSON struct {
	Errors  []string `json:"errors"`
	Updated int      `json:"updated"`
}

// handleRefresh is the handler for POST /control/upstream_subscriptions/refresh.
func (m *Manager) handleRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	req := &refreshJSON{}
	if r.ContentLength != 0 {
		err := json.NewDecoder(r.Body).Decode(req)
		if err != nil {
			aghhttp.ErrorAndLog(ctx, m.logger, r, w, http.StatusBadRequest, "decoding: %s", err)

			return
		}
	}

	var ids []uint64
	if req.ID != 0 {
		ids = []uint64{req.ID}
	}

	updated, err := m.refresh(ctx, true, ids)
	resp := &refreshRespJSON{
		Errors:  []string{},
		Updated: updated,
	}

	if err != nil {
		resp.Errors = strings.Split(err.Error(), "\n")
	}

	aghhttp.WriteJSONResponseOK(ctx, m.logger, w, r, resp)
}
