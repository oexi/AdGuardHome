package dnsforward

import (
	"context"
	"net"
	"testing"

	"github.com/AdguardTeam/AdGuardHome/internal/filtering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testUpstreamsExtender is an [UpstreamsExtender] for tests.
type testUpstreamsExtender struct {
	lines []string
}

// ExtendUpstreams implements the [UpstreamsExtender] interface for
// *testUpstreamsExtender.
func (e *testUpstreamsExtender) ExtendUpstreams(
	_ context.Context,
	ups []string,
) (extended []string) {
	return append(ups, e.lines...)
}

func TestServer_Prepare_upstreamsExtender(t *testing.T) {
	t.Parallel()

	newConf := func(ext UpstreamsExtender) (c ServerConfig) {
		return ServerConfig{
			UDPListenAddrs: []*net.UDPAddr{{}},
			TCPListenAddrs: []*net.TCPAddr{{}},
			TLSConf:        &TLSConfig{},
			Config: Config{
				UpstreamMode:     UpstreamModeLoadBalance,
				EDNSClientSubnet: &EDNSClientSubnet{Enabled: false},
				ClientsContainer: EmptyClientsContainer{},
			},
			ServePlainDNS:     true,
			UpstreamsExtender: ext,
		}
	}

	upstreamAddrs := func(s *Server) (addrs []string) {
		for _, u := range s.conf.UpstreamConfig.Upstreams {
			addrs = append(addrs, u.Address())
		}

		return addrs
	}

	testCases := []struct {
		ext           UpstreamsExtender
		name          string
		wantUpstreams []string
		wantConf      []string
	}{{
		ext:           nil,
		name:          "no_extender",
		wantUpstreams: []string{"https://dns10.quad9.net:443/dns-query"},
		wantConf:      defaultDNS,
	}, {
		ext:           &testUpstreamsExtender{lines: []string{"tls://1.1.1.1"}},
		name:          "extender_general",
		wantUpstreams: []string{"tls://1.1.1.1:853"},
		wantConf:      nil,
	}, {
		ext:           &testUpstreamsExtender{lines: []string{"[/example.org/]1.1.1.1"}},
		name:          "extender_domain_only",
		wantUpstreams: []string{"https://dns10.quad9.net:443/dns-query"},
		wantConf:      nil,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := createTestServer(t, &filtering.Config{
				BlockingMode: filtering.BlockingModeDefault,
			}, newConf(tc.ext), nil)
			require.NotNil(t, s.conf.UpstreamConfig)

			assert.Equal(t, tc.wantUpstreams, upstreamAddrs(s))
			assert.Equal(t, tc.wantConf, s.conf.UpstreamDNS)
		})
	}
}
