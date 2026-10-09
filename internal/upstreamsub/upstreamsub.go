// Package upstreamsub implements subscriptions to remote lists of upstream DNS
// servers.
package upstreamsub

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AdguardTeam/AdGuardHome/internal/agh"
	"github.com/AdguardTeam/AdGuardHome/internal/aghhttp"
	"github.com/AdguardTeam/AdGuardHome/internal/aghos"
	"github.com/AdguardTeam/AdGuardHome/internal/aghrenameio"
	"github.com/AdguardTeam/golibs/container"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
)

// DefaultMaxSize is the default maximum size of a subscription's contents.
const DefaultMaxSize = 64 << 20

// DefaultUpdateIntervalHours is the default interval between automatic
// updates, in hours.
const DefaultUpdateIntervalHours = 24

// dirName is the name of the directory within the data directory, where the
// contents of the subscriptions are stored.
const dirName = "upstream_subscriptions"

// checkInterval is the interval between checks for outdated subscriptions.
const checkInterval = 10 * time.Minute

// startDelay is the delay before the first check for outdated subscriptions,
// so that the DNS server, which is used to resolve the hostnames of the
// subscriptions, is started.
const startDelay = 10 * time.Second

// ManagerConfig is the configuration structure for a [*Manager].
type ManagerConfig struct {
	// Logger is used for logging the operation of the manager.  It must not
	// be nil.
	Logger *slog.Logger

	// Config is the initial persistent configuration.  It must not be nil.
	Config *Config

	// HTTPClient is used to download the subscriptions.  It must not be nil.
	HTTPClient *http.Client

	// ConfModifier is used to persist the configuration changes.  It must not
	// be nil.
	ConfModifier agh.ConfigModifier

	// HTTPReg is used to register the HTTP API handlers.  It must not be nil.
	HTTPReg aghhttp.Registrar

	// OnChange is called when the resulting set of upstreams changes.  It
	// must not be nil.  It's never called with the manager's lock held, so it
	// may call [Manager.ExtendUpstreams].
	OnChange func(ctx context.Context)

	// DataDir is the directory to store the contents of the subscriptions in.
	DataDir string

	// MaxSize is the maximum size of a subscription's contents in bytes.  If
	// zero, [DefaultMaxSize] is used.
	MaxSize int64
}

// subState is the runtime state of a subscription.
type subState struct {
	// res is the parsed contents.  It's nil if the contents have never been
	// loaded.
	res *parseResult

	// lastErr is the error of the last update, if any.
	lastErr error

	// checksum is the checksum of the raw contents.
	checksum [sha256.Size]byte
}

// Manager manages upstream subscriptions.
type Manager struct {
	logger       *slog.Logger
	client       *http.Client
	confModifier agh.ConfigModifier
	onChange     func(ctx context.Context)

	// done is closed when the manager is closed.
	done chan struct{}

	// mu protects conf and states.
	mu     *sync.RWMutex
	conf   *Config
	states map[uint64]*subState

	// updateMu serializes downloads.
	updateMu *sync.Mutex

	// startOnce makes sure that the update loop is started only once.
	startOnce *sync.Once

	// closeOnce makes sure that done is closed only once.
	closeOnce *sync.Once

	dir     string
	maxSize int64
}

// New returns a new properly initialized *Manager.  It loads the cached
// contents of the subscriptions from disk and registers the HTTP handlers.
// conf must not be nil and must be valid.
func New(ctx context.Context, c *ManagerConfig) (m *Manager, err error) {
	maxSize := c.MaxSize
	if maxSize <= 0 {
		maxSize = DefaultMaxSize
	}

	m = &Manager{
		logger:       c.Logger,
		client:       c.HTTPClient,
		confModifier: c.ConfModifier,
		onChange:     c.OnChange,
		done:         make(chan struct{}),
		mu:           &sync.RWMutex{},
		conf:         c.Config.clone(),
		states:       map[uint64]*subState{},
		updateMu:     &sync.Mutex{},
		startOnce:    &sync.Once{},
		closeOnce:    &sync.Once{},
		dir:          filepath.Join(c.DataDir, dirName),
		maxSize:      maxSize,
	}

	err = os.MkdirAll(m.dir, aghos.DefaultPermDir)
	if err != nil {
		return nil, fmt.Errorf("creating directory: %w", err)
	}

	for _, sub := range m.conf.Subscriptions {
		m.loadFromDisk(ctx, sub)
	}

	m.registerHandlers(c.HTTPReg)

	return m, nil
}

// path returns the path of the file with the contents of the subscription with
// the given id.
func (m *Manager) path(id uint64) (p string) {
	return filepath.Join(m.dir, strconv.FormatUint(id, 10)+".txt")
}

// loadFromDisk loads the cached contents of sub.  m.mu is expected to be
// locked or m to be not yet in use.
func (m *Manager) loadFromDisk(ctx context.Context, sub *Subscription) {
	data, err := os.ReadFile(m.path(sub.ID))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			m.logger.ErrorContext(ctx, "loading subscription", "id", sub.ID, slogutil.KeyError, err)
		}

		return
	}

	res := parse(data, sub.Upstreams)
	st := &subState{
		res:      res,
		checksum: sha256.Sum256(data),
	}

	if res.rulesCount == 0 {
		st.lastErr = fmt.Errorf("no valid rules found, %d invalid lines", res.invalidCount)
	}

	m.states[sub.ID] = st

	m.logger.DebugContext(
		ctx,
		"loaded subscription",
		"id", sub.ID,
		"rules", res.rulesCount,
		"invalid", res.invalidCount,
	)
}

// ExtendUpstreams returns ups extended with the upstreams from the enabled
// subscriptions.  The domains specified in ups, as well as in the previous
// subscriptions, take precedence over the ones from the subsequent
// subscriptions.
func (m *Manager) ExtendUpstreams(ctx context.Context, ups []string) (extended []string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.conf.Enabled {
		return ups
	}

	seenDomains := container.NewMapSet[string]()
	seenGeneral := container.NewMapSet[string]()
	for _, l := range ups {
		domainsStr, _, ok := strings.Cut(strings.TrimPrefix(l, "[/"), "/]")
		if ok && strings.HasPrefix(l, "[/") {
			for d := range strings.SplitSeq(domainsStr, "/") {
				seenDomains.Add(strings.ToLower(strings.TrimSuffix(d, ".")))
			}
		} else {
			seenGeneral.Add(l)
		}
	}

	extended = slices.Clip(ups)
	added := 0
	for _, sub := range m.conf.Subscriptions {
		st := m.states[sub.ID]
		if !sub.Enabled || st == nil || st.res == nil {
			continue
		}

		for _, g := range st.res.groups {
			domains := make([]string, 0, len(g.domains))
			for _, d := range g.domains {
				if !seenDomains.Has(d) {
					seenDomains.Add(d)
					domains = append(domains, d)
				}
			}

			if len(domains) > 0 {
				extended = append(extended, "[/"+strings.Join(domains, "/")+"/]"+g.upstreams)
				added += len(domains)
			}
		}

		for _, u := range st.res.general {
			if !seenGeneral.Has(u) {
				seenGeneral.Add(u)
				extended = append(extended, u)
				added++
			}
		}
	}

	m.logger.DebugContext(ctx, "extended upstreams", "added_rules", added)

	return extended
}

// Start starts the periodic updates of the subscriptions.
func (m *Manager) Start() {
	m.startOnce.Do(func() {
		go m.updateLoop()
	})
}

// Close stops the periodic updates.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		close(m.done)
	})
}

// updateLoop periodically updates the outdated subscriptions until m is
// closed.
func (m *Manager) updateLoop() {
	ctx := context.Background()
	defer slogutil.RecoverAndLog(ctx, m.logger)

	timer := time.NewTimer(startDelay)
	defer timer.Stop()

	for {
		select {
		case <-m.done:
			return
		case <-timer.C:
			_, _ = m.refresh(ctx, false, nil)
			timer.Reset(checkInterval)
		}
	}
}

// isOutdated returns true if sub should be updated.  m.mu is expected to be
// locked.
func (m *Manager) isOutdated(sub *Subscription, now time.Time) (ok bool) {
	st := m.states[sub.ID]
	if st == nil || st.res == nil {
		return true
	}

	ivl := m.conf.UpdateIntervalHours
	if ivl == 0 {
		return false
	}

	return now.Sub(sub.LastUpdated) >= time.Duration(ivl)*time.Hour
}

// refresh updates the enabled subscriptions.  If force is false, only the
// outdated ones are updated.  If ids is not empty, only the subscriptions with
// these identifiers are considered.  updated is the number of subscriptions,
// the contents of which have changed.  err is the joined errors of all the
// updates.
func (m *Manager) refresh(
	ctx context.Context,
	force bool,
	ids []uint64,
) (updated int, err error) {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()

	var toUpdate []*Subscription
	func() {
		m.mu.RLock()
		defer m.mu.RUnlock()

		if !m.conf.Enabled && !force {
			return
		}

		now := time.Now()
		for _, sub := range m.conf.Subscriptions {
			if len(ids) > 0 && !slices.Contains(ids, sub.ID) {
				continue
			}

			if sub.Enabled && (force || m.isOutdated(sub, now)) {
				toUpdate = append(toUpdate, sub.clone())
			}
		}
	}()

	if len(toUpdate) == 0 {
		return 0, nil
	}

	var errs []error
	succeeded := 0
	for _, sub := range toUpdate {
		changed, updErr := m.update(ctx, sub)
		if updErr != nil {
			errs = append(errs, fmt.Errorf("subscription %q: %w", sub.Name, updErr))

			continue
		}

		succeeded++
		if changed {
			updated++
		}
	}

	if succeeded > 0 {
		// Persist the new update times.
		m.confModifier.Apply(ctx)
	}

	if updated > 0 && m.isEnabled() {
		m.onChange(ctx)
	}

	return updated, errors.Join(errs...)
}

// isEnabled returns true if the subscriptions are enabled.
func (m *Manager) isEnabled() (ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.conf.Enabled
}

// update downloads and parses the contents of sub and saves the result.  sub
// must be a copy of the configured subscription.  changed is true if the
// contents have changed.  m.updateMu is expected to be locked.
func (m *Manager) update(ctx context.Context, sub *Subscription) (changed bool, err error) {
	m.logger.InfoContext(ctx, "updating subscription", "id", sub.ID, "url", sub.URL)

	data, err := m.download(ctx, sub.URL)
	if err != nil {
		m.setError(sub.ID, err)

		return false, err
	}

	res := parse(data, sub.Upstreams)
	if res.rulesCount == 0 {
		err = fmt.Errorf("no valid rules found, %d invalid lines", res.invalidCount)
		m.setError(sub.ID, err)

		return false, err
	}

	sum := sha256.Sum256(data)

	m.mu.RLock()
	prev := m.states[sub.ID]
	m.mu.RUnlock()

	changed = prev == nil || prev.res == nil || prev.checksum != sum
	if changed {
		err = m.save(sub.ID, data)
		if err != nil {
			m.setError(sub.ID, err)

			return false, err
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// The subscription could be removed or changed during the download.
	cur := m.find(sub.ID)
	if cur == nil || cur.URL != sub.URL || !slices.Equal(cur.Upstreams, sub.Upstreams) {
		return false, nil
	}

	cur.LastUpdated = time.Now()
	if changed {
		m.states[sub.ID] = &subState{res: res, checksum: sum}
	} else {
		prev.lastErr = nil
	}

	m.logger.InfoContext(
		ctx,
		"subscription updated",
		"id", sub.ID,
		"changed", changed,
		"rules", res.rulesCount,
		"invalid", res.invalidCount,
	)

	return changed, nil
}

// setError saves err as the last error of the subscription with the given id.
func (m *Manager) setError(id uint64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := m.states[id]
	if st == nil {
		st = &subState{}
		m.states[id] = st
	}

	st.lastErr = err
}

// find returns the subscription with the given id.  m.mu is expected to be
// locked.
func (m *Manager) find(id uint64) (sub *Subscription) {
	for _, s := range m.conf.Subscriptions {
		if s.ID == id {
			return s
		}
	}

	return nil
}

// download returns the contents of the subscription at u.
func (m *Manager) download(ctx context.Context, u string) (data []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		// Don't wrap the error since it's informative enough as is.
		return nil, err
	}
	defer func() { err = errors.WithDeferred(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got status code %d, want %d", resp.StatusCode, http.StatusOK)
	}

	data, err = io.ReadAll(io.LimitReader(resp.Body, m.maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	if int64(len(data)) > m.maxSize {
		return nil, fmt.Errorf("contents are larger than %d bytes", m.maxSize)
	}

	return data, nil
}

// save atomically writes data to the file of the subscription with the given
// id.
func (m *Manager) save(id uint64, data []byte) (err error) {
	f, err := aghrenameio.NewPendingFile(m.path(id), aghos.DefaultPermFile)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer func() { err = aghrenameio.WithDeferredCleanup(err, f) }()

	_, err = f.Write(data)
	if err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

// WriteDiskConfig writes the current configuration into c.  c must not be
// nil.
func (m *Manager) WriteDiskConfig(c *Config) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	*c = *m.conf.clone()
}
