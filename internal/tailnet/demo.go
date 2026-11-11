package tailnet

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// NewDemoManager runs a scripted device for `mainspring demo` and tests: it
// waits six seconds for a pretend sign-in, then joins tail4e2a.ts.net. A name
// containing "nohttps" joins a tailnet with HTTPS certificates off.
func NewDemoManager(handler http.Handler) *Manager {
	return &Manager{Handler: handler, poll: 500 * time.Millisecond, status: Status{State: Off},
		newNode: func(_, hostname, _ string) node { return &demoNode{hostname: hostname} }}
}

type demoNode struct {
	hostname string
	started  time.Time
	out      bool
}

var demoPeers = map[key.NodePublic]*ipnstate.PeerStatus{
	key.NewNode().Public(): {HostName: "macbook", OS: "macOS", Online: true},
	key.NewNode().Public(): {HostName: "iphone", OS: "iOS", Online: true},
	key.NewNode().Public(): {HostName: "devbox", OS: "linux", Online: true},
}

func (d *demoNode) Start() error { d.started = time.Now(); return nil }

func (d *demoNode) Status(context.Context) (*ipnstate.Status, error) {
	if d.out {
		return &ipnstate.Status{BackendState: "NeedsLogin"}, nil
	}
	if time.Since(d.started) < 6*time.Second {
		return &ipnstate.Status{BackendState: "NeedsLogin", AuthURL: "https://login.tailscale.com/a/7f3c2e91d4"}, nil
	}
	st := &ipnstate.Status{
		BackendState:   "Running",
		Self:           &ipnstate.PeerStatus{DNSName: d.hostname + ".tail4e2a.ts.net.", UserID: 1},
		User:           map[tailcfg.UserID]tailcfg.UserProfile{1: {LoginName: "alex@example.com"}},
		CurrentTailnet: &ipnstate.TailnetStatus{MagicDNSSuffix: "tail4e2a.ts.net"},
		TailscaleIPs:   []netip.Addr{netip.MustParseAddr("100.101.24.7")},
		Peer:           demoPeers,
	}
	if !strings.Contains(d.hostname, "nohttps") {
		st.CertDomains = []string{d.hostname + ".tail4e2a.ts.net"}
	}
	return st, nil
}

func (d *demoNode) Listen(string, string) (net.Listener, error) {
	return nil, errors.New("the demo doesn't listen on a tailnet")
}
func (d *demoNode) ListenTLS(string, string) (net.Listener, error) {
	return nil, errors.New("the demo doesn't listen on a tailnet")
}

func (d *demoNode) CertPair(ctx context.Context, _ string) ([]byte, []byte, error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-time.After(3 * time.Second):
	}
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: d.hostname}, NotBefore: time.Now(), NotAfter: time.Now().Add(90 * 24 * time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil, nil
}

func (d *demoNode) Logout(context.Context) error { d.out = true; return nil }
func (d *demoNode) Close() error                 { return nil }
