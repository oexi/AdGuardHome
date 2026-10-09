package upstreamsub

import (
	"bufio"
	"bytes"
	"net"
	"net/netip"
	"slices"
	"strings"

	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/AdguardTeam/golibs/container"
	"github.com/AdguardTeam/golibs/netutil"
)

// group is a set of domains routed to the same upstreams.
type group struct {
	// upstreams is the space-separated list of upstream addresses, or "#" for
	// the domains that should be excluded from the more general
	// specifications.
	upstreams string

	// domains are the domain names in the form accepted by the upstream
	// configuration syntax, e.g. "example.org" or "*.example.org".
	domains []string
}

// parseResult is the result of parsing the subscription's contents.
type parseResult struct {
	// groups are the domain-specific rules in the order of the first
	// appearance of their upstreams.
	groups []*group

	// general are the upstreams that are not bound to any domain.
	general []string

	// rulesCount is the number of the valid rules.
	rulesCount int

	// invalidCount is the number of the lines that were skipped because they
	// could not be recognized or contained invalid data.
	invalidCount int
}

// lineKind is the kind of a recognized subscription line.
type lineKind uint8

const (
	lineKindSkip lineKind = iota
	lineKindInvalid
	lineKindGeneral
	lineKindDomains
)

// parse parses data into rules.  If override is not empty, all domain-specific
// rules are routed to the upstreams from override, the lines containing bare
// domain names are treated as domain-specific rules, and general upstreams are
// ignored.  The following formats are supported, line by line:
//
//   - AdGuard Home upstream syntax, e.g. "[/example.org/]1.1.1.1" or
//     "tls://dns.example";
//   - dnsmasq server directives, e.g. "server=/example.org/1.1.1.1#53";
//   - bare domain names, e.g. "example.org", "domain:example.org",
//     "full:example.org", or "+.example.org", if override is not empty.
//
// General upstreams must either have a scheme or be IP addresses with optional
// ports, e.g. "tls://dns.example" or "8.8.8.8:53".
//
// Empty lines and lines starting with "#", "!", or "//" are ignored.
func parse(data []byte, override []string) (res *parseResult) {
	res = &parseResult{}

	overrideUps := strings.Join(override, " ")
	groupIdx := map[string]*group{}
	seen := container.NewMapSet[string]()
	validUps := map[string]bool{}

	s := bufio.NewScanner(bytes.NewReader(data))
	s.Buffer(make([]byte, 0, 64*1024), maxLineLen)
	for s.Scan() {
		kind, ups, domains := parseLine(s.Text(), overrideUps != "")
		switch kind {
		case lineKindSkip:
			continue
		case lineKindInvalid:
			res.invalidCount++

			continue
		case lineKindGeneral:
			if overrideUps != "" {
				// General upstreams make no sense for domain lists.
				continue
			}

			if !isValidUpstreams(validUps, ups) {
				res.invalidCount++

				continue
			}

			if !slices.Contains(res.general, ups) {
				res.general = append(res.general, ups)
				res.rulesCount++
			}

			continue
		default:
			// Go on.
		}

		if overrideUps != "" && ups != "#" {
			ups = overrideUps
		}

		if !isValidUpstreams(validUps, ups) {
			res.invalidCount++

			continue
		}

		res.addDomains(groupIdx, seen, ups, domains)
	}

	if s.Err() != nil {
		// The only possible error is a too long line, so count it as invalid
		// and use what has been parsed so far.
		res.invalidCount++
	}

	return res
}

// maxLineLen is the maximum length of a single line in the subscription.
const maxLineLen = 16 * 1024 * 1024

// addDomains adds valid domains that haven't been seen before to the group of
// ups.
func (res *parseResult) addDomains(
	groupIdx map[string]*group,
	seen *container.MapSet[string],
	ups string,
	domains []string,
) {
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSuffix(d, "."))
		if d == "" || netutil.ValidateDomainName(strings.TrimPrefix(d, "*.")) != nil {
			res.invalidCount++

			continue
		}

		if seen.Has(d) {
			continue
		}

		seen.Add(d)

		g := groupIdx[ups]
		if g == nil {
			g = &group{upstreams: ups}
			groupIdx[ups] = g
			res.groups = append(res.groups, g)
		}

		g.domains = append(g.domains, d)
		res.rulesCount++
	}
}

// parseLine recognizes a single line.  domainList tells if bare domain names
// should be accepted.
func parseLine(line string, domainList bool) (kind lineKind, ups string, domains []string) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' || line[0] == '!' || strings.HasPrefix(line, "//") {
		return lineKindSkip, "", nil
	}

	if rest, ok := strings.CutPrefix(line, "server="); ok {
		return parseDnsmasqServer(rest)
	}

	if strings.HasPrefix(line, "[/") {
		return parseUpstreamLine(line)
	}

	if strings.Contains(line, "=") {
		// Other dnsmasq directives, such as ipset= or address=.
		return lineKindSkip, "", nil
	}

	if domainList {
		return parseDomainLine(line)
	}

	if !isExplicitUpstream(line) {
		return lineKindInvalid, "", nil
	}

	return lineKindGeneral, line, nil
}

// isExplicitUpstream returns true if line is an upstream address with a scheme
// or an IP address with an optional port.  Bare hostnames are not accepted to
// avoid treating the lines of domain lists as plain DNS upstreams.
func isExplicitUpstream(line string) (ok bool) {
	if strings.ContainsAny(line, " \t") {
		return false
	}

	if strings.Contains(line, "://") {
		return true
	}

	if _, err := netip.ParseAddrPort(line); err == nil {
		return true
	}

	_, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))

	return err == nil
}

// parseUpstreamLine parses a domain-specific line in AdGuard Home upstream
// syntax.
func parseUpstreamLine(line string) (kind lineKind, ups string, domains []string) {
	domainsStr, upsStr, ok := strings.Cut(line[len("[/"):], "/]")
	upsFields := strings.Fields(upsStr)
	if !ok || len(upsFields) == 0 {
		return lineKindInvalid, "", nil
	}

	return lineKindDomains, strings.Join(upsFields, " "), strings.Split(domainsStr, "/")
}

// parseDnsmasqServer parses the value of a dnsmasq server directive.
func parseDnsmasqServer(val string) (kind lineKind, ups string, domains []string) {
	val = strings.TrimSpace(val)
	if !strings.HasPrefix(val, "/") {
		ups = dnsmasqAddr(val)
		if ups == "" {
			return lineKindInvalid, "", nil
		}

		return lineKindGeneral, ups, nil
	}

	lastSlash := strings.LastIndexByte(val, '/')
	domainsStr, srv := val[1:lastSlash], val[lastSlash+1:]
	if domainsStr == "" {
		return lineKindInvalid, "", nil
	}

	switch srv {
	case "":
		// Local-only domains, which have no equivalent.
		return lineKindSkip, "", nil
	case "#":
		ups = "#"
	default:
		ups = dnsmasqAddr(srv)
		if ups == "" {
			return lineKindInvalid, "", nil
		}
	}

	return lineKindDomains, ups, strings.Split(domainsStr, "/")
}

// dnsmasqAddr converts the dnsmasq server address of form "ip[#port][@...]"
// into the upstream address.  It returns an empty string if addr is invalid.
func dnsmasqAddr(addr string) (ups string) {
	addr, _, _ = strings.Cut(addr, "@")
	host, port, hasPort := strings.Cut(addr, "#")

	ip, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}

	if !hasPort {
		if ip.Is6() {
			return "[" + ip.String() + "]"
		}

		return ip.String()
	}

	return net.JoinHostPort(ip.String(), port)
}

// parseDomainLine parses a line containing a bare domain name in one of the
// common formats.
func parseDomainLine(line string) (kind lineKind, ups string, domains []string) {
	for _, p := range []string{"domain:", "full:", "+.", "."} {
		if rest, ok := strings.CutPrefix(line, p); ok {
			line = rest

			break
		}
	}

	if strings.ContainsAny(line, ": \t,") {
		// Unsupported types, such as "regexp:" or "keyword:", as well as
		// trailing attributes.
		return lineKindInvalid, "", nil
	}

	return lineKindDomains, "", []string{line}
}

// isValidUpstreams returns true if all the space-separated upstreams in ups are
// valid.  cache is used to avoid validating the same upstreams repeatedly.
func isValidUpstreams(cache map[string]bool, ups string) (ok bool) {
	if ups == "#" {
		return true
	}

	if ok, cached := cache[ups]; cached {
		return ok
	}

	ok = true
	for _, u := range strings.Fields(ups) {
		if u == "#" || validateUpstream(u) != nil {
			ok = false

			break
		}
	}

	cache[ups] = ok

	return ok
}

// validateUpstream returns an error if addr is not a valid upstream address.
func validateUpstream(addr string) (err error) {
	u, err := upstream.AddressToUpstream(addr, &upstream.Options{})
	if err != nil {
		return err
	}

	return u.Close()
}
