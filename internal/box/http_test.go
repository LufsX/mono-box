package box

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

func TestHTTPConnectivityDNS(t *testing.T) {
	// LinkProperties.toString uses slash-prefixed InetAddress values; the
	// address list is not a list of host:port endpoints.
	modern := `
NetworkFactories for: WIFI TELEPHONY
Active default network: 101
Current Networks:
  NetworkAgentInfo{network{100} handle{123} ni{[type: MOBILE[LTE], state: CONNECTED/CONNECTED]} lp{{InterfaceName: rmnet_data0 LinkAddresses: [192.0.2.2/24] DnsAddresses: [ /192.0.2.53 ] Routes: [0.0.0.0/0 -> 192.0.2.1 rmnet_data0]}}
  NetworkAgentInfo{network{101} handle{456} ni{[type: WIFI[], state: CONNECTED/CONNECTED]} lp{{InterfaceName: wlan0 LinkAddresses: [192.0.2.20/24,2001:db8::20/64] DnsAddresses: [ /2001:db8::53 /192.0.2.54 /2001:db8::53 ] Domains: null MTU: 1500}}
  NetworkAgentInfo{network{102} handle{789} ni{[type: VPN[], state: CONNECTED/CONNECTED]} lp{{InterfaceName: tun0 DnsAddresses: [ /192.0.2.55 ]}}
Network Requests:
  NetworkRequest [ REQUEST id=1, [ Capabilities: INTERNET&NOT_RESTRICTED ] ]
Network transition logs:
  NetworkAgentInfo{network{101} lp{{InterfaceName: old0 DnsAddresses: [ /192.0.2.99 ]}}}
`
	legacy := `
Active default network: 105
Current Networks:
  NetworkAgentInfo [MOBILE (LTE) - 100] CONNECTED/CONNECTED
    LinkProperties: {InterfaceName: rmnet0 LinkAddresses: [192.0.2.1/24] DnsAddresses: [/192.0.2.53]}
  NetworkAgentInfo [WIFI () - 105] CONNECTED/CONNECTED
    LinkProperties: {InterfaceName: wlan0 LinkAddresses: [2001:db8::2/64]
      DnsAddresses: [/fe80::1, /fe80::2%wlan0, /192.0.2.54] Domains: null}
Network Requests:
`
	for _, tc := range []struct {
		name, dump string
		want       []string
	}{
		{"modern active Wi-Fi", modern, []string{"2001:db8::53", "192.0.2.54"}},
		{"active cellular", strings.Replace(modern, "Active default network: 101", "Active default network: 100", 1), []string{"192.0.2.53"}},
		{"active VPN", strings.Replace(modern, "Active default network: 101", "Active default network: 102", 1), []string{"192.0.2.55"}},
		{"legacy multiline link-local", legacy, []string{"fe80::1%wlan0", "fe80::2%wlan0", "192.0.2.54"}},
		{"without section heading", "Active default network: 101\n  NetworkAgentInfo{network{101} lp{{InterfaceName: wlan0 DnsAddresses: [ /2001:db8::53 ]}}}", []string{"2001:db8::53"}},
		{"CRLF", strings.ReplaceAll(modern, "\n", "\r\n"), []string{"2001:db8::53", "192.0.2.54"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAndroidConnectivityDNS(tc.dump)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct{ name, dump string }{
		{"no default", strings.Replace(modern, "Active default network: 101", "Active default network: none", 1)},
		{"missing record", strings.Replace(modern, "Active default network: 101", "Active default network: 999", 1)},
		{"empty default DNS", strings.Replace(modern, "[ /2001:db8::53 /192.0.2.54 /2001:db8::53 ]", "[]", 1)},
		{"history only", "Active default network: 101\nCurrent Networks:\nNetwork transition logs:\n  NetworkAgentInfo{network{101} lp{{DnsAddresses: [ /192.0.2.99 ]}}}"},
		{"unassociated properties", "Active default network: 101\nNetworkAgentInfo{network{100} lp{{DnsAddresses: [ /192.0.2.53 ]}}}\nDnsAddresses: [ /192.0.2.99 ]"},
		{"no LinkProperties", "Active default network: 101\nNetworkAgentInfo{network{101} DnsAddresses: [ /192.0.2.99 ]}"},
		{"DNS outside LinkProperties", "Active default network: 101\nNetworkAgentInfo{network{101} lp{{InterfaceName: wlan0}}}\nDnsAddresses: [ /192.0.2.99 ]"},
		{"incomplete LinkProperties", "Active default network: 101\nNetworkAgentInfo{network{101} lp{{DnsAddresses: [ /192.0.2.99 ]"},
		{"historical duplicate", "Active default network: 101\nNetworkAgentInfo{network{101} lp{{DnsAddresses: []}}}\nNetworkAgentInfo{network{101} lp{{DnsAddresses: [ /192.0.2.99 ]}}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := parseAndroidConnectivityDNS(tc.dump); err == nil {
				t.Fatalf("accepted %v", got)
			}
		})
	}
}

func TestHTTPDNSAddressesAndProperties(t *testing.T) {
	got := parseAndroidDNSAddresses("/192.0.2.53, /2001:db8::53 /fe80::1%wlan0 ::ffff:192.0.2.53 :: 0.0.0.0 ff02::1 invalid 192.0.2.1:53 /fe80::2", "")
	want := []string{"192.0.2.53", "2001:db8::53", "fe80::1%wlan0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	props := `
[ro.product.model]: [test]
[net.dns2]: [2001:db8::53]
[net.wlan0.dns1]: [192.0.2.99]
[net.rmnet0.dns1]: [192.0.2.98]
[net.dns10]: [192.0.2.54]
[net.dns1]: [192.0.2.53]
[net.dns3]: [192.0.2.53]
[net.dns4]: [not-an-address]
`
	got, err := parseAndroidPropertyDNS(props)
	want = []string{"192.0.2.53", "2001:db8::53", "192.0.2.54"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, props := range []string{"", "[net.dns1]: []", "[net.dns1]: [::]", "[net.wlan0.dns1]: [192.0.2.53]"} {
		if _, err := parseAndroidPropertyDNS(props); err == nil {
			t.Fatalf("accepted %q", props)
		}
	}
}

func TestHTTPDNSDiscovery(t *testing.T) {
	dump := "Active default network: 100\nNetworkAgentInfo{network{100} lp{{DnsAddresses: [ /192.0.2.53 /2001:db8::53 ]}}}"
	for _, tc := range []struct {
		name, dump, props string
		dumpErr, propErr  error
		wantCalls         int
		want              []string
	}{
		{"active", dump, "", nil, nil, 1, []string{"192.0.2.53", "2001:db8::53"}},
		{"properties after permission denial", "", "[net.dns1]: [192.0.2.54]", fs.ErrPermission, nil, 2, []string{"192.0.2.54"}},
		{"properties after empty DNS", "Active default network: 100", "[net.dns1]: [2001:db8::54]", nil, nil, 2, []string{"2001:db8::54"}},
		{"neither available", "", "", fs.ErrPermission, fs.ErrNotExist, 2, nil},
		{"no addresses", "Permission Denial: can't dump ConnectivityService", "[net.wlan0.dns1]: [192.0.2.53]", nil, nil, 2, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
				calls++
				if calls == 1 {
					if name != "/system/bin/dumpsys" || !reflect.DeepEqual(args, []string{"connectivity"}) {
						t.Fatalf("unexpected command %s %v", name, args)
					}
					return []byte(tc.dump), tc.dumpErr
				}
				if name != "/system/bin/getprop" || len(args) != 0 {
					t.Fatalf("unexpected command %s %v", name, args)
				}
				return []byte(tc.props), tc.propErr
			}
			got, err := discoverAndroidDNS(context.Background(), run)
			if calls != tc.wantCalls || !reflect.DeepEqual(got, tc.want) || (err != nil) != (tc.want == nil) {
				t.Fatalf("got %v, %v, calls %d", got, err, calls)
			}
			if err != nil && (!strings.Contains(err.Error(), "dumpsys connectivity:") || !strings.Contains(err.Error(), "getprop:")) {
				t.Fatal(err)
			}
		})
	}
}

func TestHTTPDNSCache(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			var calls atomic.Int32
			started, release := make(chan struct{}), make(chan struct{})
			clock := time.Now()
			discoveryErr := errors.New("permission denied")
			cache := &androidDNSCache{
				now: func() time.Time { return clock },
				discover: func(ctx context.Context) ([]string, error) {
					if calls.Add(1) == 1 {
						close(started)
						<-release
					}
					if fail {
						return nil, discoveryErr
					}
					return []string{"192.0.2.53"}, nil
				},
			}
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					got, err := cache.lookup(context.Background())
					if fail {
						if !errors.Is(err, discoveryErr) {
							t.Errorf("error %v", err)
						}
					} else if err != nil || !reflect.DeepEqual(got, []string{"192.0.2.53"}) {
						t.Errorf("got %v, %v", got, err)
					}
				}()
			}
			<-started
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := cache.lookup(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("waiting caller cancellation: %v", err)
			}
			close(release)
			wg.Wait()
			if calls.Load() != 1 {
				t.Fatalf("discoveries: %d", calls.Load())
			}
			if !fail {
				got, _ := cache.lookup(context.Background())
				got[0] = "modified by caller"
				got, _ = cache.lookup(context.Background())
				if got[0] != "192.0.2.53" {
					t.Fatal("cache aliased caller's slice")
				}
			}
			clock = clock.Add(androidDNSCacheTTL)
			_, _ = cache.lookup(context.Background())
			if calls.Load() != 2 {
				t.Fatal("cache did not expire")
			}
		})
	}
}

func TestHTTPDNSCacheCanceledRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	cache := &androidDNSCache{
		now: time.Now,
		discover: func(ctx context.Context) ([]string, error) {
			calls++
			if calls == 1 {
				cancel()
				return nil, ctx.Err()
			}
			return []string{"192.0.2.53"}, nil
		},
	}
	if _, err := cache.lookup(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	got, err := cache.lookup(context.Background())
	if err != nil || calls != 2 || !reflect.DeepEqual(got, []string{"192.0.2.53"}) {
		t.Fatalf("cancellation poisoned cache: %v, %v, calls %d", got, err, calls)
	}
}

func httpTestCA(t *testing.T) ([]byte, tls.Certificate) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "HTTP test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, root, root, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2), DNSNames: []string{"download.test"},
		NotBefore: root.NotBefore, NotAfter: root.NotAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, root, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		tls.Certificate{Certificate: [][]byte{leafDER, der}, PrivateKey: key}
}

func httpTestCertFS(pemData []byte) fstest.MapFS {
	return fstest.MapFS{"apex/com.android.conscrypt/cacerts/test.0": &fstest.MapFile{Data: pemData}}
}

type httpUnreadableCertFS struct{ fs.FS }

func (f httpUnreadableCertFS) Open(name string) (fs.File, error) {
	if strings.HasSuffix(name, "unreadable.0") {
		return nil, fs.ErrPermission
	}
	return f.FS.Open(name)
}

func TestHTTPAndroidRoots(t *testing.T) {
	primary, _ := httpTestCA(t)
	fallback, _ := httpTestCA(t)
	for _, tc := range []struct {
		name  string
		files fs.FS
		want  []byte
	}{
		{"APEX preferred", fstest.MapFS{
			"apex/com.android.conscrypt/cacerts/a.0": &fstest.MapFile{Data: primary},
			"system/etc/security/cacerts/b.0":        &fstest.MapFile{Data: fallback},
		}, primary},
		{"legacy", fstest.MapFS{"system/etc/security/cacerts/b.0": &fstest.MapFile{Data: fallback}}, fallback},
		{"invalid APEX fallback", fstest.MapFS{
			"apex/com.android.conscrypt/cacerts/a.0": &fstest.MapFile{Data: []byte("invalid")},
			"system/etc/security/cacerts/b.0":        &fstest.MapFile{Data: fallback},
		}, fallback},
		{"skip unreadable and junk", httpUnreadableCertFS{fstest.MapFS{
			"apex/com.android.conscrypt/cacerts/unreadable.0": &fstest.MapFile{Data: fallback},
			"apex/com.android.conscrypt/cacerts/good.0":       &fstest.MapFile{Data: primary},
			"apex/com.android.conscrypt/cacerts/junk":         &fstest.MapFile{Data: []byte("junk")},
		}}, primary},
		{"unreadable APEX fallback", httpUnreadableCertFS{fstest.MapFS{
			"apex/com.android.conscrypt/cacerts/unreadable.0": &fstest.MapFile{Data: primary},
			"system/etc/security/cacerts/b.0":                 &fstest.MapFile{Data: fallback},
		}}, fallback},
		{"missing", fstest.MapFS{}, nil},
		{"invalid", httpTestCertFS([]byte("invalid")), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool, err := loadAndroidHTTPRoots(tc.files)
			if tc.want == nil {
				if err == nil || pool == nil || len(pool.Subjects()) != 0 || !strings.Contains(err.Error(), "Android system CA") {
					t.Fatalf("pool %v, error %v", pool, err)
				}
				return
			}
			expected := x509.NewCertPool()
			expected.AppendCertsFromPEM(tc.want)
			if err != nil || !pool.Equal(expected) {
				t.Fatalf("unexpected trust store: %v", err)
			}
		})
	}
}

// Serve real DNS packets over UDP and TCP without an external resolver or an
// extra module dependency. The UDP truncation case exercises Go's TCP fallback.
func httpTestDNS(t *testing.T, truncateUDP bool) (string, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		tcp.Close()
		t.Fatal(err)
	}
	var udpCount, tcpCount atomic.Int32
	t.Cleanup(func() { udp.Close(); tcp.Close() })
	reply := func(q []byte, truncated bool) []byte {
		if len(q) < 17 {
			return nil
		}
		end := 12
		for end < len(q) && q[end] != 0 {
			end += int(q[end]) + 1
		}
		end += 5 // terminating zero plus QTYPE and QCLASS
		if end > len(q) {
			return nil
		}
		resp := make([]byte, 12)
		copy(resp[:2], q[:2])
		binary.BigEndian.PutUint16(resp[2:4], 0x8180)
		binary.BigEndian.PutUint16(resp[4:6], 1)
		resp = append(resp, q[12:end]...)
		if truncated {
			resp[2] |= 2
			return resp
		}
		qtype := binary.BigEndian.Uint16(q[end-4 : end-2])
		if qtype == 1 || qtype == 28 {
			binary.BigEndian.PutUint16(resp[6:8], 1)
			resp = append(resp, 0xc0, 0x0c, 0, byte(qtype), 0, 1, 0, 0, 0, 60)
			if qtype == 1 {
				resp = append(resp, 0, 4, 127, 0, 0, 1)
			} else {
				resp = append(resp, 0, 16)
				resp = append(resp, net.IPv6loopback...)
			}
		}
		return resp
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			udpCount.Add(1)
			_, _ = udp.WriteTo(reply(buf[:n], truncateUDP), addr)
		}
	}()
	go func() {
		for {
			conn, err := tcp.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				var size [2]byte
				if _, err := io.ReadFull(conn, size[:]); err != nil {
					return
				}
				q := make([]byte, binary.BigEndian.Uint16(size[:]))
				if _, err := io.ReadFull(conn, q); err != nil {
					return
				}
				tcpCount.Add(1)
				resp := reply(q, false)
				binary.BigEndian.PutUint16(size[:], uint16(len(resp)))
				_, _ = conn.Write(append(size[:], resp...))
			}()
		}
	}()
	return tcp.Addr().String(), &udpCount, &tcpCount
}

func TestHTTPAndroidTLSAndDNS(t *testing.T) {
	for _, truncate := range []bool{false, true} {
		t.Run(map[bool]string{false: "UDP", true: "TCP fallback"}[truncate], func(t *testing.T) {
			root, cert := httpTestCA(t)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/error" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, _ = io.WriteString(w, "verified")
			}))
			server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
			server.StartTLS()
			defer server.Close()
			dnsAddr, udp, tcp := httpTestDNS(t, truncate)
			var discoveries atomic.Int32
			var wrongTarget atomic.Bool
			discover := func(context.Context) ([]string, error) {
				discoveries.Add(1)
				return []string{"2001:db8::53"}, nil
			}
			dialDNS := func(ctx context.Context, network, address string) (net.Conn, error) {
				if address != "[2001:db8::53]:53" {
					wrongTarget.Store(true)
				}
				return (&net.Dialer{}).DialContext(ctx, network, dnsAddr)
			}
			client := newAndroidHTTPClient(httpTestCertFS(root), discover, dialDNS)
			defer client.CloseIdleConnections()
			_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
			endpoint := "https://download.test:" + port
			resp, err := client.Get(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || string(body) != "verified" || discoveries.Load() != 1 || wrongTarget.Load() || udp.Load() < 2 {
				t.Fatalf("body %q err %v discoveries %d udp %d wrong target %v", body, err, discoveries.Load(), udp.Load(), wrongTarget.Load())
			}
			if truncate && tcp.Load() < 2 {
				t.Fatal("missing DNS TCP fallback")
			}
			resp, err = client.Get(endpoint + "/error")
			if err != nil || resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("HTTP status must reach caller: %v %v", resp, err)
			}
			resp.Body.Close()
			if discoveries.Load() != 1 {
				t.Fatal("keep-alive unexpectedly repeated discovery")
			}
			_, err = client.Get("https://wrong.test:" + port)
			var hostnameErr x509.HostnameError
			if !errors.As(err, &hostnameErr) {
				t.Fatalf("hostname verification: %v", err)
			}
			otherRoot, _ := httpTestCA(t)
			untrusted := newAndroidHTTPClient(httpTestCertFS(otherRoot), discover, dialDNS)
			defer untrusted.CloseIdleConnections()
			_, err = untrusted.Get(endpoint)
			var authorityErr x509.UnknownAuthorityError
			if !errors.As(err, &authorityErr) {
				t.Fatalf("CA verification: %v", err)
			}
		})
	}
}

func TestHTTPAndroidDNSFailureAndFallback(t *testing.T) {
	dnsAddr, _, _ := httpTestDNS(t, false)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	var discoveries, failedServer, secondServer atomic.Int32
	discover := func(context.Context) ([]string, error) {
		discoveries.Add(1)
		return []string{"192.0.2.53", "2001:db8::53"}, nil
	}
	dialDNS := func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == "192.0.2.53:53" {
			failedServer.Add(1)
			return nil, errors.New("system server unavailable")
		}
		if address != "[2001:db8::53]:53" {
			t.Errorf("unexpected DNS target %s", address)
		}
		secondServer.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, dnsAddr)
	}
	client := newAndroidHTTPClient(fstest.MapFS{}, discover, dialDNS)
	defer client.CloseIdleConnections()
	resp, err := client.Get("http://download.test:" + port)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if discoveries.Load() != 1 || failedServer.Load() == 0 || secondServer.Load() == 0 {
		t.Fatal("did not discover once and fail over to second system server")
	}
	for _, tc := range []struct {
		name     string
		discover httpDNSDiscovery
		dial     httpDial
		want     string
	}{
		{"discovery error", func(context.Context) ([]string, error) { return nil, fs.ErrPermission }, dialDNS, "Android system DNS discovery"},
		{"empty discovery", func(context.Context) ([]string, error) { return nil, nil }, dialDNS, "no nameservers"},
		{"all servers fail", discover, func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("offline") }, "Android system DNS lookup failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newAndroidHTTPClient(fstest.MapFS{}, tc.discover, tc.dial)
			defer client.CloseIdleConnections()
			if _, err := client.Get("http://download.test:" + port); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestHTTPAndroidIPv6(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Listener.Close()
	server.Listener = listener
	server.Start()
	defer server.Close()
	dnsAddr, _, _ := httpTestDNS(t, false)
	client := newAndroidHTTPClient(fstest.MapFS{}, func(context.Context) ([]string, error) {
		return []string{"2001:db8::53"}, nil
	}, func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, dnsAddr)
	})
	defer client.CloseIdleConnections()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	for _, endpoint := range []string{"http://download.test:" + port, server.URL} {
		resp, err := client.Get(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatal(resp.Status)
		}
	}
}

func TestHTTPAndroidSilentDNSFallback(t *testing.T) {
	silent, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	dnsAddr, _, _ := httpTestDNS(t, false)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	var discoveries atomic.Int32
	client := newAndroidHTTPClient(fstest.MapFS{}, func(context.Context) ([]string, error) {
		discoveries.Add(1)
		return []string{"192.0.2.53", "2001:db8::53"}, nil
	}, func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == "192.0.2.53:53" {
			return (&net.Dialer{}).DialContext(ctx, network, silent.LocalAddr().String())
		}
		return (&net.Dialer{}).DialContext(ctx, network, dnsAddr)
	})
	defer client.CloseIdleConnections()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	resp, err := client.Get("http://download.test:" + port)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if discoveries.Load() != 1 {
		t.Fatal("server retry repeated discovery")
	}
}

func TestHTTPDirectLocalAPIAndMissingCA(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing local API credential")
		}
		_, _ = io.WriteString(w, "{}")
	}))
	defer server.Close()
	client := newAndroidHTTPClient(fstest.MapFS{}, func(context.Context) ([]string, error) {
		t.Error("literal IP must bypass DNS discovery")
		return nil, errors.New("DNS unavailable")
	}, nil)
	defer client.CloseIdleConnections()
	_, portText, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := net.LookupPort("tcp", portText)
	r := &Runtime{Config: Config{APIPort: port, APISecret: "secret"}, HTTP: client}
	if _, err := r.apiRequest(http.MethodGet, "/configs", nil); err != nil {
		t.Fatal(err)
	}
	_, err := client.Get("https://download.test/file")
	if err == nil || !strings.Contains(err.Error(), "Android system CA certificates unavailable") {
		t.Fatalf("missing CA error: %v", err)
	}
	hostTransport := newDirectHTTPTransport()
	defer hostTransport.CloseIdleConnections()
	if hostTransport.Proxy != nil || hostTransport.DialContext == nil || hostTransport.TLSHandshakeTimeout == 0 || !hostTransport.ForceAttemptHTTP2 {
		t.Fatal("host must retain default transport settings with no environment proxy")
	}
	if http.DefaultTransport.(*http.Transport).Proxy == nil {
		t.Fatal("changed global default transport")
	}
}

func TestHTTPCommandBounds(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MONOBOX_HTTP_TEST_HELPER", "1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runAndroidHTTPCommand(ctx, exe, "-test.run=^TestHTTPCommandHelper$", "--", "wait"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	start := time.Now()
	if _, err := runAndroidHTTPCommand(context.Background(), exe, "-test.run=^TestHTTPCommandHelper$", "--", "wait"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("execution timeout: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("command timeout was not bounded")
	}
	if _, err := runAndroidHTTPCommand(context.Background(), exe, "-test.run=^TestHTTPCommandHelper$", "--", "large"); err == nil {
		t.Fatal("accepted excessive command output")
	}
	out := &httpCommandOutput{limit: 3}
	if _, err := out.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte("d")); err == nil || out.String() != "abc" {
		t.Fatal("output bound failed")
	}
}

func TestHTTPCommandHelper(t *testing.T) {
	if os.Getenv("MONOBOX_HTTP_TEST_HELPER") != "1" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "wait":
		time.Sleep(10 * time.Second)
	case "large":
		_, _ = io.WriteString(os.Stdout, strings.Repeat("x", 5<<20))
	}
	os.Exit(0)
}
