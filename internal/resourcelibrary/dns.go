package resourcelibrary

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"time"
)

const packageDNSServerEnv = "AGENTDOCK_RESOURCE_LIBRARY_DNS_SERVER"

// packageLookup uses system DNS by default. An explicitly configured public
// DNS IP is a narrow opt-in for fake-IP TUN setups (Quantumult X, Clash, etc).
// It resolves only resource library transfer origins. All resulting answers
// still undergo the exact same global-public IP checks before any dial, and
// HTTPS certificate verification and origin locking remain mandatory.
// This is not an implicit DNS leak or fallback to untrusted alternate hosts.
func packageLookup(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	configured := strings.TrimSpace(os.Getenv(packageDNSServerEnv))
	if configured == "" {
		return systemLookup(ctx, host)
	}
	serverIP, err := packageDNSServerIP(configured)
	if err != nil {
		return nil, err
	}
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 5 * time.Second}
			return dialer.DialContext(ctx, network, net.JoinHostPort(serverIP.String(), "53"))
		},
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	results := make([]net.IP, 0, len(addresses))
	for _, a := range addresses {
		if a.IP != nil {
			results = append(results, a.IP)
		}
	}
	if len(results) == 0 {
		return nil, errPackageDial
	}
	return results, nil
}

func packageDNSServerIP(value string) (net.IP, error) {
	address := net.ParseIP(strings.TrimSpace(value))
	if address == nil || !publicIP(address) {
		return nil, errors.New("resource library DNS override must be a public literal IP")
	}
	return address, nil
}
