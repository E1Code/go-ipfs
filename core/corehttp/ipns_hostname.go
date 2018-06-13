package corehttp

import (
	"context"
	"net"
	"net/http"
	"strings"

	core "github.com/ipsn/go-ipfs/core"
	nsopts "github.com/ipsn/go-ipfs/namesys/opts"

	isd "github.com/jbenet/go-is-domain"
)

// IPNSHostnameOption rewrites an incoming request if its Host: header contains
// an IPNS name.
// The rewritten request points at the resolved name on the gateway handler.
func IPNSHostnameOption() ServeOption {
	return func(n *core.IpfsNode, _ net.Listener, mux *http.ServeMux) (*http.ServeMux, error) {
		childMux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithCancel(n.Context())
			defer cancel()

			host := strings.SplitN(r.Host, ":", 2)[0]
			if len(host) > 0 && isd.IsDomain(host) {
				name := "/ipns/" + host
				if _, err := n.Namesys.Resolve(ctx, name, nsopts.Depth(1)); err == nil {
					r.Header.Set("X-Ipns-Original-Path", r.URL.Path)
					r.URL.Path = name + r.URL.Path
				}
			}
			childMux.ServeHTTP(w, r)
		})
		return childMux, nil
	}
}
