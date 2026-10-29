package base

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const androidDNSCacheTTL = 5 * time.Second

type httpDNSDiscovery func(context.Context) ([]string, error)
type httpCommand func(context.Context, string, ...string) ([]byte, error)
type httpDial func(context.Context, string, string) (net.Conn, error)

// NewHTTPClient also works in GOOS=linux, CGO_ENABLED=0 Android binaries.
// Use a fixed device path: PATH and the build target do not identify Android.
func NewHTTPClient() *http.Client {
	info, err := os.Stat("/system/bin/getprop")
	if err != nil || info.IsDir() {
		return &http.Client{Transport: NewDirectHTTPTransport()}
	}
	cache := &androidDNSCache{
		discover: func(ctx context.Context) ([]string, error) {
			return discoverAndroidDNS(ctx, runAndroidHTTPCommand)
		},
		now: time.Now,
	}
	return newAndroidHTTPClient(os.DirFS("/"), cache.lookup, (&net.Dialer{}).DialContext)
}

func NewDirectHTTPTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	// The same client carries local API credentials and remote downloads.
	t.Proxy = nil
	return t
}

func newAndroidHTTPClient(certs fs.FS, discover httpDNSDiscovery, dialDNS httpDial) *http.Client {
	t := NewDirectHTTPTransport()
	pool, err := loadAndroidHTTPRoots(certs)
	// A non-nil, empty pool must never fall back to Linux roots or SSL_CERT_*.
	t.TLSClientConfig = &tls.Config{RootCAs: pool}
	t.DialContext = androidHTTPDialContext(discover, dialDNS)
	return &http.Client{Transport: &androidHTTPTransport{Transport: t, rootsErr: err}}
}

type androidHTTPTransport struct {
	*http.Transport
	rootsErr error
}

func (t *androidHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "https" && t.rootsErr != nil {
		return nil, t.rootsErr
	}
	return t.Transport.RoundTrip(req)
}

// Only the first usable system store is trusted. Older Android releases do not
// have the Conscrypt APEX store. User-installed roots are deliberately optional.
func loadAndroidHTTPRoots(certs fs.FS) (*x509.CertPool, error) {
	var failures []error
	for _, dir := range []string{"apex/com.android.conscrypt/cacerts", "system/etc/security/cacerts"} {
		pool := x509.NewCertPool()
		entries, err := fs.ReadDir(certs, dir)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", dir, err))
			continue
		}
		loaded := false
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := fs.ReadFile(certs, path.Join(dir, entry.Name()))
			if err != nil {
				failures = append(failures, fmt.Errorf("%s/%s: %w", dir, entry.Name(), err))
				continue
			}
			if pool.AppendCertsFromPEM(data) {
				loaded = true
			}
		}
		if loaded {
			return pool, nil
		}
		failures = append(failures, fmt.Errorf("%s: no readable PEM certificates", dir))
	}
	return x509.NewCertPool(), fmt.Errorf("Android system CA certificates unavailable: %w", errors.Join(failures...))
}

// Each TCP connection takes one DNS snapshot, shared by parallel A/AAAA lookups
// and retries. Literal IPs (especially the local API) need no discovery at all.
func androidHTTPDialContext(discover httpDNSDiscovery, dialDNS httpDial) httpDial {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		dialer := &net.Dialer{KeepAlive: 30 * time.Second}
		if _, err := netip.ParseAddr(host); err == nil {
			return dialer.DialContext(ctx, network, address)
		}
		servers, err := discover(ctx)
		if err != nil {
			return nil, fmt.Errorf("Android system DNS discovery: %w", err)
		}
		if len(servers) == 0 {
			return nil, errors.New("Android system DNS unavailable: no nameservers")
		}
		var failures []error
		for _, server := range servers {
			// Pin both UDP and TCP (including truncated-response fallback) to
			// this server. Go's resolv.conf addresses are never used.
			dialer.Resolver = &net.Resolver{
				PreferGo: true,
				Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
					return dialDNS(ctx, network, net.JoinHostPort(server, "53"))
				},
			}
			// Bound an unresponsive server, then try the next system server.
			// net.Dialer retains its normal dual-stack / Happy Eyeballs logic.
			attempt, stop := context.WithTimeout(ctx, 5*time.Second)
			conn, err := dialer.DialContext(attempt, network, address)
			stop()
			if err == nil {
				return conn, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var dnsErr *net.DNSError
			if !errors.As(err, &dnsErr) {
				return nil, err
			}
			failures = append(failures, fmt.Errorf("DNS %s: %w", server, err))
		}
		return nil, fmt.Errorf("Android system DNS lookup failed: %w", errors.Join(failures...))
	}
}

// Coalesce simultaneous dials; cache failures too so permission failures do not
// spawn dumpsys for every query. Waiting callers retain their own cancellation.
type androidDNSCache struct {
	mu       sync.Mutex
	discover httpDNSDiscovery
	now      func() time.Time
	inflight chan struct{}
	expires  time.Time
	servers  []string
	err      error
}

func (c *androidDNSCache) lookup(ctx context.Context) ([]string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		if c.now().Before(c.expires) {
			servers, err := append([]string(nil), c.servers...), c.err
			c.mu.Unlock()
			return servers, err
		}
		if pending := c.inflight; pending != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pending:
				continue
			}
		}
		c.inflight = make(chan struct{})
		c.mu.Unlock()

		servers, err := c.discover(ctx)
		c.mu.Lock()
		// A canceled caller must not cache its cancellation for other dials.
		if ctx.Err() == nil {
			c.servers, c.err = append([]string(nil), servers...), err
			c.expires = c.now().Add(androidDNSCacheTTL)
		}
		close(c.inflight)
		c.inflight = nil
		c.mu.Unlock()
		return servers, err
	}
}

func discoverAndroidDNS(ctx context.Context, run httpCommand) ([]string, error) {
	data, dumpErr := run(ctx, "/system/bin/dumpsys", "connectivity")
	if dumpErr == nil {
		var servers []string
		servers, dumpErr = parseAndroidConnectivityDNS(string(data))
		if dumpErr == nil {
			return servers, nil
		}
	}
	data, propErr := run(ctx, "/system/bin/getprop")
	if propErr == nil {
		var servers []string
		servers, propErr = parseAndroidPropertyDNS(string(data))
		if propErr == nil {
			return servers, nil
		}
	}
	return nil, fmt.Errorf("no usable system nameservers: %w", errors.Join(
		fmt.Errorf("dumpsys connectivity: %w", dumpErr),
		fmt.Errorf("getprop: %w", propErr),
	))
}

// Bound both execution and output. No shell, PATH lookup, curl or netd protocol
// assumptions are needed. WaitDelay also bounds pipes inherited by descendants.
func runAndroidHTTPCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 100 * time.Millisecond
	out := &httpCommandOutput{limit: 4 << 20}
	stderr := &httpCommandOutput{limit: 4096}
	cmd.Stdout, cmd.Stderr = out, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}

type httpCommandOutput struct {
	// Do not embed bytes.Buffer: its promoted ReadFrom would let io.Copy
	// bypass the bounded Write method when exec drains command output.
	buffer bytes.Buffer
	limit  int
}

func (b *httpCommandOutput) Bytes() []byte  { return b.buffer.Bytes() }
func (b *httpCommandOutput) String() string { return b.buffer.String() }

func (b *httpCommandOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, errors.New("Android command output exceeds limit")
	}
	return b.buffer.Write(p)
}

var (
	httpActiveNetwork = regexp.MustCompile(`(?m)^[ \t]*Active default network:[ \t]*([0-9]+)[ \t]*\r?$`)
	httpNetworkRecord = regexp.MustCompile(`(?m)^[ \t]*NetworkAgentInfo[ \t]*[\[{]`)
	httpNetworkID     = regexp.MustCompile(`\bnetwork\{([0-9]+)\}`)
	httpOldNetworkID  = regexp.MustCompile(`NetworkAgentInfo[ \t]*\{?[ \t]*\[[^\]\r\n]* - ([0-9]+)\]`)
	httpDNSAddresses  = regexp.MustCompile(`\bDnsAddresses:[ \t]*\[([^\]]*)\]`)
	httpInterfaceName = regexp.MustCompile(`\bInterfaceName:[ \t]*([^ \t\r\n{}]+)`)
	httpDNSProperty   = regexp.MustCompile(`^\[net\.dns([1-9][0-9]*)\]:[ \t]*\[([^\]]*)\]$`)
)

// Parse current NetworkAgentInfo records, never DNS in requests, event logs,
// tethering, or an unrelated network. Absence of an active ID is not a license
// to pick the first Wi-Fi/mobile record.
func parseAndroidConnectivityDNS(dump string) ([]string, error) {
	active := httpActiveNetwork.FindStringSubmatch(dump)
	if active == nil {
		return nil, errors.New("active default network not reported")
	}
	// Current Networks precedes diagnostic history on Android releases that
	// print this heading. Stop at the next heading of the same indentation.
	lines := strings.Split(dump, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "Current Networks:" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		end := i + 1
		for ; end < len(lines); end++ {
			text := strings.TrimSpace(lines[end])
			if text != "" && len(lines[end])-len(strings.TrimLeft(lines[end], " \t")) <= indent {
				break
			}
		}
		dump = strings.Join(lines[i+1:end], "\n")
		break
	}
	records := httpNetworkRecord.FindAllStringIndex(dump, -1)
	for i, record := range records {
		end := len(dump)
		if i+1 < len(records) {
			end = records[i+1][0]
		}
		block := dump[record[0]:end]
		id := httpNetworkID.FindStringSubmatch(block)
		if id == nil {
			id = httpOldNetworkID.FindStringSubmatch(block)
		}
		if id == nil || id[1] != active[1] {
			continue
		}
		// Only the record's LinkProperties can supply its DNS addresses.
		lp := strings.Index(block, "lp{")
		if lp < 0 {
			lp = strings.Index(block, "LinkProperties:")
		}
		if lp < 0 {
			break
		}
		// Isolate the balanced LinkProperties object. Later diagnostic
		// output must not supply DNS for a record whose own list is empty.
		start := strings.IndexByte(block[lp:], '{')
		if start < 0 {
			break
		}
		start += lp
		depth, end := 0, start
		for ; end < len(block); end++ {
			switch block[end] {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth == 0 {
				break
			}
		}
		if end == len(block) {
			break
		}
		block = block[start : end+1]
		dns := httpDNSAddresses.FindStringSubmatch(block)
		if dns == nil {
			break
		}
		iface := ""
		if match := httpInterfaceName.FindStringSubmatch(block); match != nil {
			iface = match[1]
		}
		servers := parseAndroidDNSAddresses(dns[1], iface)
		if len(servers) > 0 {
			return servers, nil
		}
		break
	}
	return nil, fmt.Errorf("active default network %s has no usable LinkProperties DNS addresses", active[1])
}

func parseAndroidDNSAddresses(value, iface string) []string {
	var servers []string
	seen := make(map[string]bool)
	for _, token := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
	}) {
		ip, err := netip.ParseAddr(strings.Trim(token, "/[]"))
		if err != nil || ip.IsUnspecified() || ip.IsMulticast() {
			continue
		}
		if zone := ip.Zone(); zone != "" && strings.ContainsAny(zone, "%/[]:") {
			continue
		}
		ip = ip.Unmap()
		if ip.Is6() && ip.IsLinkLocalUnicast() && (ip.Zone() == "" || ip.Zone() == "0") {
			if iface == "" {
				continue
			}
			ip = ip.WithZone(iface)
		}
		server := ip.String()
		if !seen[server] {
			seen[server] = true
			servers = append(servers, server)
		}
	}
	return servers
}

// Legacy global properties only. net.<interface>.dnsN can belong to an inactive
// network; without an active association it is unsafe to use those properties.
func parseAndroidPropertyDNS(properties string) ([]string, error) {
	type property struct {
		index int
		value string
	}
	var values []property
	for _, line := range strings.Split(properties, "\n") {
		if match := httpDNSProperty.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			index, err := strconv.Atoi(match[1])
			if err == nil {
				values = append(values, property{index, match[2]})
			}
		}
	}
	sort.SliceStable(values, func(i, j int) bool { return values[i].index < values[j].index })
	var tokens []string
	for _, value := range values {
		tokens = append(tokens, value.value)
	}
	servers := parseAndroidDNSAddresses(strings.Join(tokens, " "), "")
	if len(servers) == 0 {
		return nil, errors.New("net.dnsN properties contain no usable addresses")
	}
	return servers, nil
}
