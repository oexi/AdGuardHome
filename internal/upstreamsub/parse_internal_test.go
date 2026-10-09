package upstreamsub

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		data         string
		override     []string
		wantGroups   []*group
		wantGeneral  []string
		wantRules    int
		wantInvalids int
	}{{
		name: "adguard_syntax",
		data: "# comment\n" +
			"\n" +
			"tls://dns.example\n" +
			"dns.example\n" +
			"[2001:db8::1]:53\n" +
			"[/example.org/Example.NET/]1.1.1.1  8.8.8.8\n" +
			"[/*.example.com/]#\n" +
			"[/bad_domain!/]1.1.1.1\n" +
			"[/example.org/]9.9.9.9\n",
		wantGroups: []*group{{
			upstreams: "1.1.1.1 8.8.8.8",
			domains:   []string{"example.org", "example.net"},
		}, {
			upstreams: "#",
			domains:   []string{"*.example.com"},
		}},
		wantGeneral:  []string{"tls://dns.example", "[2001:db8::1]:53"},
		wantRules:    5,
		wantInvalids: 2,
	}, {
		name: "dnsmasq",
		data: "server=/example.org/114.114.114.114\n" +
			"server=/a.example/b.example/114.114.114.114#5353\n" +
			"server=/v6.example/2001:db8::1\n" +
			"server=/std.example/#\n" +
			"server=/local.example/\n" +
			"server=/bad.example/not-an-ip\n" +
			"ipset=/example.org/set\n" +
			"server=8.8.8.8\n",
		wantGroups: []*group{{
			upstreams: "114.114.114.114",
			domains:   []string{"example.org"},
		}, {
			upstreams: "114.114.114.114:5353",
			domains:   []string{"a.example", "b.example"},
		}, {
			upstreams: "[2001:db8::1]",
			domains:   []string{"v6.example"},
		}, {
			upstreams: "#",
			domains:   []string{"std.example"},
		}},
		wantGeneral:  []string{"8.8.8.8"},
		wantRules:    6,
		wantInvalids: 1,
	}, {
		name: "dnsmasq_override",
		data: "server=/example.org/114.114.114.114\n" +
			"server=/example.net/223.5.5.5\n" +
			"server=8.8.8.8\n",
		override: []string{"https://dns.alidns.com/dns-query", "119.29.29.29"},
		wantGroups: []*group{{
			upstreams: "https://dns.alidns.com/dns-query 119.29.29.29",
			domains:   []string{"example.org", "example.net"},
		}},
		wantRules: 2,
	}, {
		name: "domain_list",
		data: "example.org\n" +
			"domain:example.net\n" +
			"full:www.example.com\n" +
			"+.example.info\n" +
			"regexp:^ads\\.\n" +
			"example.org\n",
		override: []string{"1.1.1.1"},
		wantGroups: []*group{{
			upstreams: "1.1.1.1",
			domains:   []string{"example.org", "example.net", "www.example.com", "example.info"},
		}},
		wantRules:    4,
		wantInvalids: 1,
	}, {
		name:         "domain_list_no_override",
		data:         "example.org\nexample.net\n",
		wantRules:    0,
		wantInvalids: 2,
	}, {
		name:         "invalid_upstream",
		data:         "[/example.org/]bad://upstream\nfoo bar\n",
		wantRules:    0,
		wantInvalids: 2,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := parse([]byte(tc.data), tc.override)
			assert.Equal(t, tc.wantGroups, res.groups)
			assert.Equal(t, tc.wantGeneral, res.general)
			assert.Equal(t, tc.wantRules, res.rulesCount)
			assert.Equal(t, tc.wantInvalids, res.invalidCount)
		})
	}
}
