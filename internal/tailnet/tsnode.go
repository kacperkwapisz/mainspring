package tailnet

import (
	"context"
	"net"

	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
)

// tsNode is the real device: tsnet in userspace networking mode, so it needs
// no TUN device or extra permissions (it works inside Umbrel's container).
type tsNode struct {
	srv *tsnet.Server
	lc  *local.Client
}

func newTSNode(dir, hostname, authKey string) node {
	return &tsNode{srv: &tsnet.Server{
		Dir:      dir,
		Hostname: hostname,
		AuthKey:  authKey,
		Logf:     func(string, ...any) {}, // tsnet's own log is very chatty
		UserLogf: func(string, ...any) {}, // Mainspring logs the sign-in link itself
	}}
}

func (n *tsNode) Start() error {
	if err := n.srv.Start(); err != nil {
		return err
	}
	lc, err := n.srv.LocalClient()
	if err != nil {
		return err
	}
	n.lc = lc
	return nil
}

func (n *tsNode) Status(ctx context.Context) (*ipnstate.Status, error) {
	return n.lc.Status(ctx)
}

func (n *tsNode) Listen(network, addr string) (net.Listener, error) {
	return n.srv.Listen(network, addr)
}

func (n *tsNode) ListenTLS(network, addr string) (net.Listener, error) {
	return n.srv.ListenTLS(network, addr)
}

func (n *tsNode) CertPair(ctx context.Context, domain string) ([]byte, []byte, error) {
	return n.lc.CertPair(ctx, domain)
}

func (n *tsNode) Logout(ctx context.Context) error { return n.lc.Logout(ctx) }

func (n *tsNode) Close() error { return n.srv.Close() }
