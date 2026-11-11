// Package tailnet puts Mainspring on the operator's Tailscale network as its
// own device (tsnet), with an HTTPS address like
// https://mainspring.tail4e2a.ts.net that every device on the tailnet can reach.
package tailnet

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"tailscale.com/ipn/ipnstate"
)

// States the panel walks through.
const (
	Off           = "off"            // not on a tailnet
	Starting      = "starting"       // the device is starting up
	NeedsLogin    = "needs_login"    // waiting for the operator to approve AuthURL
	NeedsApproval = "needs_approval" // signed in; a tailnet admin must approve the device
	Certificate   = "certificate"    // joined; fetching the HTTPS certificate
	NoHTTPS       = "no_https"       // joined, but the tailnet has HTTPS certificates off
	Connected     = "connected"      // joined, with HTTPS
	Failed        = "failed"
)

// Peer is another device on the tailnet, for the diagram in Settings.
type Peer struct {
	Name   string `json:"name"`
	OS     string `json:"os,omitempty"`
	Online bool   `json:"online"`
}

// Status is what the panel shows.
type Status struct {
	State       string `json:"state"`
	Hostname    string `json:"hostname,omitempty"`
	AuthURL     string `json:"auth_url,omitempty"`
	Tailnet     string `json:"tailnet,omitempty"` // MagicDNS suffix, like tail4e2a.ts.net
	User        string `json:"user,omitempty"`
	DNSName     string `json:"dns_name,omitempty"`
	IP          string `json:"ip,omitempty"`
	HTTPS       bool   `json:"https"`
	CertExpires int64  `json:"cert_expires,omitempty"` // unix ms
	Peers       []Peer `json:"peers,omitempty"`
	Error       string `json:"error,omitempty"`
}

// URL is the address the panel and tools use, once known.
func (s Status) URL() string {
	if s.DNSName == "" {
		return ""
	}
	if s.State == Connected {
		return "https://" + s.DNSName
	}
	return "http://" + s.DNSName
}

// node is the Tailscale device; the real one is tsnet, tests use a fake.
type node interface {
	Start() error
	Status(ctx context.Context) (*ipnstate.Status, error)
	Listen(network, addr string) (net.Listener, error)
	ListenTLS(network, addr string) (net.Listener, error)
	CertPair(ctx context.Context, domain string) (certPEM, keyPEM []byte, err error)
	Logout(ctx context.Context) error
	Close() error
}

// Manager runs at most one device.
type Manager struct {
	// Dir holds the device's Tailscale state (its keys), so it stays signed in
	// across restarts.
	Dir string
	// Handler serves requests that arrive over the tailnet.
	Handler http.Handler
	// OnChange is called whenever Status changes.
	OnChange func()
	// Logf receives one line per state change, like the sign-in link.
	Logf func(format string, args ...any)

	newNode func(dir, hostname, authKey string) node
	poll    time.Duration

	mu      sync.Mutex
	node    node
	status  Status
	cancel  context.CancelFunc
	servers []*http.Server
	wantTLS bool

	// The HTTPS certificate, tracked apart from Status so a poll in flight
	// can't undo a finished fetch.
	certDomain  string
	certExpires int64
	certErr     string
	fetching    bool
	retryAt     time.Time
}

// NewManager builds a manager that runs a real tsnet device.
func NewManager(dir string, handler http.Handler) *Manager {
	return &Manager{Dir: dir, Handler: handler, newNode: newTSNode, poll: time.Second, status: Status{State: Off}}
}

var hostnameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidHostname reports whether name can be the device's name.
func ValidHostname(name string) bool { return hostnameRe.MatchString(name) }

// Status returns the current status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	s.Peers = append([]Peer(nil), s.Peers...)
	return s
}

// Start brings the device up as hostname. With an auth key it joins without a
// browser sign-in; without one, Status reports a sign-in link.
func (m *Manager) Start(hostname, authKey string) error {
	if !ValidHostname(hostname) {
		return errors.New("use lowercase letters, numbers and dashes, up to 63 characters")
	}
	m.Stop()
	n := m.newNode(m.Dir, hostname, authKey)
	if err := n.Start(); err != nil {
		m.set(Status{State: Failed, Hostname: hostname, Error: err.Error()})
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.node, m.cancel = n, cancel
	m.mu.Unlock()
	m.set(Status{State: Starting, Hostname: hostname})
	go m.run(ctx, n, hostname)
	return nil
}

// Recheck looks at the tailnet again now, after the operator turned HTTPS on.
func (m *Manager) Recheck() {
	m.mu.Lock()
	n := m.node
	m.mu.Unlock()
	if n != nil {
		m.tick(context.Background(), n, m.Status().Hostname)
	}
}

// Stop shuts the device down but keeps it signed in.
func (m *Manager) Stop() {
	m.mu.Lock()
	n, cancel, servers := m.node, m.cancel, m.servers
	m.node, m.cancel, m.servers, m.wantTLS = nil, nil, nil, false
	m.certDomain, m.certExpires, m.certErr, m.fetching, m.retryAt = "", 0, "", false, time.Time{}
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, s := range servers {
		_ = s.Close()
	}
	if n != nil {
		_ = n.Close()
	}
}

// Remove signs the device out of the tailnet and forgets it. Signing out
// gets five seconds; the device is forgotten either way.
func (m *Manager) Remove(ctx context.Context) error {
	m.mu.Lock()
	n := m.node
	m.mu.Unlock()
	if n != nil {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = n.Logout(c)
		cancel()
	}
	return m.Forget()
}

// Forget shuts the device down and deletes its state without signing out,
// for a device that never joined.
func (m *Manager) Forget() error {
	m.Stop()
	if m.Dir != "" {
		if err := os.RemoveAll(m.Dir); err != nil {
			return err
		}
	}
	m.set(Status{State: Off})
	return nil
}

func (m *Manager) run(ctx context.Context, n node, hostname string) {
	t := time.NewTicker(m.poll)
	defer t.Stop()
	for {
		m.tick(ctx, n, hostname)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (m *Manager) tick(ctx context.Context, n node, hostname string) {
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	st, err := n.Status(c)
	cancel()
	if err != nil {
		if ctx.Err() == nil {
			m.set(Status{State: Starting, Hostname: hostname})
		}
		return
	}
	next := Status{Hostname: hostname}
	switch st.BackendState {
	case "NeedsLogin":
		next.State, next.AuthURL = NeedsLogin, st.AuthURL
		if st.AuthURL == "" {
			next.State = Starting
		}
	case "NeedsMachineAuth":
		next.State = NeedsApproval
	case "Running":
		m.describe(&next, st)
		m.serve(n)
		if len(st.CertDomains) == 0 {
			next.State = NoHTTPS
			break
		}
		next.HTTPS = true
		domain := st.CertDomains[0]
		m.mu.Lock()
		if m.certDomain != domain {
			m.certDomain, m.certExpires, m.certErr, m.retryAt = domain, 0, "", time.Time{}
		}
		if m.certExpires > time.Now().UnixMilli() {
			next.State, next.CertExpires = Connected, m.certExpires
		} else {
			next.State, next.Error = Certificate, m.certErr
			if !m.fetching && !time.Now().Before(m.retryAt) {
				m.fetching = true
				go m.fetchCert(ctx, n, domain)
			}
		}
		m.mu.Unlock()
	default:
		next.State = Starting
	}
	m.set(next)
}

// describe fills in where the device is and who else is on the tailnet.
func (m *Manager) describe(s *Status, st *ipnstate.Status) {
	if st.Self != nil {
		s.DNSName = strings.TrimSuffix(st.Self.DNSName, ".")
		if p, ok := st.User[st.Self.UserID]; ok {
			s.User = p.LoginName
		}
	}
	if st.CurrentTailnet != nil {
		s.Tailnet = st.CurrentTailnet.MagicDNSSuffix
	}
	for _, ip := range st.TailscaleIPs {
		if ip.Is4() {
			s.IP = ip.String()
			break
		}
	}
	var peers []Peer
	for _, p := range st.Peer {
		if p == nil || p.HostName == "" {
			continue
		}
		peers = append(peers, Peer{Name: p.HostName, OS: p.OS, Online: p.Online})
	}
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].Online != peers[j].Online {
			return peers[i].Online
		}
		return peers[i].Name < peers[j].Name
	})
	if len(peers) > 3 {
		peers = peers[:3]
	}
	s.Peers = peers
}

func (m *Manager) fetchCert(ctx context.Context, n node, domain string) {
	c, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	certPEM, _, err := n.CertPair(c, domain)
	m.mu.Lock()
	m.fetching = false
	if m.node != n || m.certDomain != domain {
		m.mu.Unlock()
		return
	}
	if err != nil {
		m.certErr = "Couldn't get the HTTPS certificate yet: " + err.Error()
		m.retryAt = time.Now().Add(30 * time.Second)
	} else {
		m.certErr, m.certExpires = "", certExpiry(certPEM)
	}
	m.mu.Unlock()
	m.tick(ctx, n, m.Status().Hostname)
}

func certExpiry(certPEM []byte) int64 {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return time.Now().Add(24 * time.Hour).UnixMilli()
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Now().Add(24 * time.Hour).UnixMilli()
	}
	return cert.NotAfter.UnixMilli()
}

// serve starts listening on the tailnet once the device is up: HTTPS on 443,
// and plain HTTP on 80, which moves the panel to HTTPS once it works but keeps
// answering API calls so tools that use http:// aren't redirected mid-request.
func (m *Manager) serve(n node) {
	m.mu.Lock()
	if len(m.servers) > 0 || m.node != n {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	var servers []*http.Server
	if ln, err := n.Listen("tcp", ":80"); err == nil {
		s := &http.Server{Handler: m.plainHandler(), ReadHeaderTimeout: 15 * time.Second}
		servers = append(servers, s)
		go func() { _ = s.Serve(ln) }()
	}
	if ln, err := n.ListenTLS("tcp", ":443"); err == nil {
		s := &http.Server{Handler: m.Handler, ReadHeaderTimeout: 15 * time.Second}
		servers = append(servers, s)
		go func() { _ = s.Serve(ln) }()
	}
	m.mu.Lock()
	m.servers = servers
	m.mu.Unlock()
}

func (m *Manager) plainHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.Status().State == Connected && !isAPI(r.URL.Path) {
			http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusPermanentRedirect)
			return
		}
		m.Handler.ServeHTTP(w, r)
	})
}

func isAPI(path string) bool {
	for _, p := range []string{"/v1/", "/v0/", "/mcp", "/health"} {
		if strings.HasPrefix(path, p) || path == strings.TrimSuffix(p, "/") {
			return true
		}
	}
	return false
}

func (m *Manager) set(s Status) {
	m.mu.Lock()
	same := statusEqual(m.status, s)
	if !same && s.State != m.status.State && m.Logf != nil {
		switch s.State {
		case NeedsLogin:
			m.Logf("tailnet: sign in to add %s to your tailnet: %s", s.Hostname, s.AuthURL)
		case Connected:
			m.Logf("tailnet: Mainspring is at https://%s", s.DNSName)
		case NoHTTPS:
			m.Logf("tailnet: joined as %s, but the tailnet has HTTPS certificates off", s.DNSName)
		}
	}
	m.status = s
	m.mu.Unlock()
	if !same {
		m.changed()
	}
}

func (m *Manager) changed() {
	if m.OnChange != nil {
		m.OnChange()
	}
}

func statusEqual(a, b Status) bool {
	if a.State != b.State || a.Hostname != b.Hostname || a.AuthURL != b.AuthURL || a.Tailnet != b.Tailnet ||
		a.User != b.User || a.DNSName != b.DNSName || a.IP != b.IP || a.HTTPS != b.HTTPS ||
		a.CertExpires != b.CertExpires || a.Error != b.Error || len(a.Peers) != len(b.Peers) {
		return false
	}
	for i := range a.Peers {
		if a.Peers[i] != b.Peers[i] {
			return false
		}
	}
	return true
}
