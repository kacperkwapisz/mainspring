package tailnet

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

type fakeNode struct {
	mu                sync.Mutex
	st                *ipnstate.Status
	certErr           error
	listened          []string
	loggedOut, closed bool
	notAfter          time.Time
}

func (f *fakeNode) Start() error { return nil }
func (f *fakeNode) Status(context.Context) (*ipnstate.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *f.st
	return &cp, nil
}
func (f *fakeNode) listen(addr string) (net.Listener, error) {
	f.mu.Lock()
	f.listened = append(f.listened, addr)
	f.mu.Unlock()
	return net.Listen("tcp", "127.0.0.1:0")
}
func (f *fakeNode) Listen(_, addr string) (net.Listener, error)    { return f.listen(addr) }
func (f *fakeNode) ListenTLS(_, addr string) (net.Listener, error) { return f.listen("tls" + addr) }
func (f *fakeNode) CertPair(context.Context, string) ([]byte, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.certErr != nil {
		return nil, nil, f.certErr
	}
	return selfSigned(f.notAfter), nil, nil
}
func (f *fakeNode) Logout(context.Context) error { f.loggedOut = true; return nil }
func (f *fakeNode) Close() error                 { f.closed = true; return nil }
func (f *fakeNode) set(fn func(*ipnstate.Status)) {
	f.mu.Lock()
	fn(f.st)
	f.mu.Unlock()
}

func selfSigned(notAfter time.Time) []byte {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mainspring"}, NotBefore: time.Now(), NotAfter: notAfter}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func running(st *ipnstate.Status, certDomains ...string) {
	st.BackendState, st.AuthURL = "Running", ""
	st.Self = &ipnstate.PeerStatus{DNSName: "mainspring.tail4e2a.ts.net.", UserID: 7}
	st.User = map[tailcfg.UserID]tailcfg.UserProfile{7: {LoginName: "alex@example.com"}}
	st.CurrentTailnet = &ipnstate.TailnetStatus{MagicDNSSuffix: "tail4e2a.ts.net"}
	st.TailscaleIPs = []netip.Addr{netip.MustParseAddr("100.101.24.7")}
	st.Peer = map[key.NodePublic]*ipnstate.PeerStatus{
		key.NewNode().Public(): {HostName: "macbook", OS: "macOS", Online: true},
		key.NewNode().Public(): {HostName: "old-pi", OS: "linux", Online: false},
	}
	st.CertDomains = certDomains
}

func manager(t *testing.T, f *fakeNode) *Manager {
	t.Helper()
	m := NewManager(filepath.Join(t.TempDir(), "tailscale"), http.NotFoundHandler())
	m.poll = 10 * time.Millisecond
	m.newNode = func(string, string, string) node { return f }
	t.Cleanup(m.Stop)
	return m
}

func waitFor(t *testing.T, m *Manager, state string) Status {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s := m.Status(); s.State == state {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("never reached %s; at %+v", state, m.Status())
	return Status{}
}

func TestSignInThenHTTPS(t *testing.T) {
	f := &fakeNode{st: &ipnstate.Status{BackendState: "NeedsLogin", AuthURL: "https://login.tailscale.com/a/7f3c2e91"}, notAfter: time.Now().Add(90 * 24 * time.Hour)}
	m := manager(t, f)
	changes := 0
	var cmu sync.Mutex
	m.OnChange = func() { cmu.Lock(); changes++; cmu.Unlock() }
	if err := m.Start("mainspring", ""); err != nil {
		t.Fatal(err)
	}
	if s := waitFor(t, m, NeedsLogin); s.AuthURL != "https://login.tailscale.com/a/7f3c2e91" || s.URL() != "" {
		t.Fatalf("%+v", s)
	}
	f.set(func(st *ipnstate.Status) { running(st, "mainspring.tail4e2a.ts.net") })
	s := waitFor(t, m, Connected)
	if s.URL() != "https://mainspring.tail4e2a.ts.net" || s.IP != "100.101.24.7" || s.User != "alex@example.com" || s.Tailnet != "tail4e2a.ts.net" {
		t.Fatalf("%+v", s)
	}
	if s.CertExpires < time.Now().Add(80*24*time.Hour).UnixMilli() || !s.HTTPS {
		t.Fatalf("certificate expiry not read: %+v", s)
	}
	if len(s.Peers) != 2 || s.Peers[0].Name != "macbook" {
		t.Fatalf("online peers first: %+v", s.Peers)
	}
	f.mu.Lock()
	listened := append([]string(nil), f.listened...)
	f.mu.Unlock()
	if len(listened) != 2 {
		t.Fatalf("should listen on :80 and TLS :443 once: %v", listened)
	}
	cmu.Lock()
	if changes == 0 {
		t.Fatal("OnChange never called")
	}
	cmu.Unlock()
}

func TestHTTPSOffThenOn(t *testing.T) {
	f := &fakeNode{st: &ipnstate.Status{}, notAfter: time.Now().Add(90 * 24 * time.Hour)}
	running(f.st)
	m := manager(t, f)
	_ = m.Start("mainspring", "")
	if s := waitFor(t, m, NoHTTPS); s.URL() != "http://mainspring.tail4e2a.ts.net" {
		t.Fatalf("%+v", s)
	}
	f.set(func(st *ipnstate.Status) { st.CertDomains = []string{"mainspring.tail4e2a.ts.net"} })
	m.Recheck()
	waitFor(t, m, Connected)
}

func TestCertificateFailureIsShownAndRetried(t *testing.T) {
	f := &fakeNode{st: &ipnstate.Status{}, certErr: os.ErrDeadlineExceeded}
	running(f.st, "mainspring.tail4e2a.ts.net")
	m := manager(t, f)
	_ = m.Start("mainspring", "")
	deadline := time.Now().Add(2 * time.Second)
	for m.Status().Error == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s := m.Status(); s.State != Certificate || s.Error == "" {
		t.Fatalf("%+v", s)
	}
}

func TestRemoveSignsOutAndForgets(t *testing.T) {
	f := &fakeNode{st: &ipnstate.Status{}}
	running(f.st)
	m := manager(t, f)
	_ = os.MkdirAll(m.Dir, 0o700)
	_ = m.Start("mainspring", "")
	waitFor(t, m, NoHTTPS)
	if err := m.Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !f.loggedOut || !f.closed || m.Status().State != Off {
		t.Fatalf("logout %v close %v state %s", f.loggedOut, f.closed, m.Status().State)
	}
	if _, err := os.Stat(m.Dir); !os.IsNotExist(err) {
		t.Fatal("state left behind")
	}
}

func TestPlainHTTPMovesThePanelButNotTheAPI(t *testing.T) {
	m := &Manager{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), status: Status{State: Connected}}
	h := m.plainHandler()
	for path, want := range map[string]int{"/": 308, "/#/pool": 308, "/v1/models": 204, "/mcp": 204, "/health": 204, "/v0/management/usage": 204} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://mainspring.tail4e2a.ts.net"+path, nil))
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
	m.status.State = NoHTTPS
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://mainspring.tail4e2a.ts.net/", nil))
	if rec.Code != 204 {
		t.Fatalf("without HTTPS the panel stays on http: %d", rec.Code)
	}
}

func TestHostnames(t *testing.T) {
	for name, ok := range map[string]bool{"mainspring": true, "ms-2": true, "Mainspring": false, "-x": false, "x-": false, "a.b": false, "": false} {
		if ValidHostname(name) != ok {
			t.Errorf("%q: %v", name, !ok)
		}
	}
}
