package upstreamsub

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/AdguardTeam/golibs/errors"
)

// Config is the persistent configuration of upstream subscriptions.
type Config struct {
	// Subscriptions are the configured subscriptions in the order of their
	// priority.
	Subscriptions []*Subscription `yaml:"subscriptions"`

	// UpdateIntervalHours is the interval between automatic updates of the
	// subscriptions, in hours.  Zero disables automatic updates.
	UpdateIntervalHours uint32 `yaml:"update_interval"`

	// Enabled tells if the upstreams from the subscriptions are used.
	Enabled bool `yaml:"enabled"`
}

// clone returns a deep copy of c.
func (c *Config) clone() (cloned *Config) {
	cloned = &Config{
		UpdateIntervalHours: c.UpdateIntervalHours,
		Enabled:             c.Enabled,
		Subscriptions:       make([]*Subscription, 0, len(c.Subscriptions)),
	}

	for _, sub := range c.Subscriptions {
		cloned.Subscriptions = append(cloned.Subscriptions, sub.clone())
	}

	return cloned
}

// Subscription is a single subscription to a remote list of upstreams.
type Subscription struct {
	// LastUpdated is the time of the last successful update of the
	// subscription.
	LastUpdated time.Time `yaml:"last_updated,omitempty"`

	// Name is the human-readable name of the subscription.
	Name string `yaml:"name"`

	// URL is the HTTP(S) URL of the list.
	URL string `yaml:"url"`

	// Upstreams, if not empty, replace the upstreams of all the
	// domain-specific rules of the subscription.  It also makes the lines
	// containing bare domain names to be treated as rules for these upstreams.
	Upstreams []string `yaml:"upstreams,omitempty"`

	// ID is the unique identifier of the subscription.
	ID uint64 `yaml:"id"`

	// Enabled tells if the subscription is used.
	Enabled bool `yaml:"enabled"`
}

// clone returns a deep copy of sub.
func (sub *Subscription) clone() (cloned *Subscription) {
	cloned = &Subscription{}
	*cloned = *sub
	cloned.Upstreams = slices.Clone(sub.Upstreams)

	return cloned
}

// validateURL returns an error if u is not a valid subscription URL.
func validateURL(u string) (err error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("url: %w", err)
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		// Go on.
	default:
		return fmt.Errorf("url: scheme %q is not supported, want http or https", parsed.Scheme)
	}

	if parsed.Host == "" {
		return fmt.Errorf("url: %w", errors.Error("no host"))
	}

	return nil
}

// normalizeUpstreams returns the non-empty upstreams from ups and validates
// them.  ups may contain several space-separated upstreams in each element.
func normalizeUpstreams(ups []string) (normalized []string, err error) {
	for _, u := range ups {
		for f := range strings.FieldsSeq(u) {
			if strings.HasPrefix(f, "#") {
				break
			}

			err = validateUpstream(f)
			if err != nil {
				return nil, fmt.Errorf("upstream %q: %w", f, err)
			}

			if !slices.Contains(normalized, f) {
				normalized = append(normalized, f)
			}
		}
	}

	return normalized, nil
}
