package netutil

import (
	"net"
	"strings"
)

// IsLocalOrPrivateHost reports whether host denotes a loopback, private/LAN,
// link-local or otherwise non-public endpoint, that is one that does not send
// corpus content off the machine or the operator's network. A bare single-label
// hostname (no dot, e.g. "gpu-vps") is treated as LAN. Public FQDNs and public
// IPs return false. It is the one classifier behind the doctor's egress check
// and the transport rules for self-hosted provider endpoints (SPEC 8.1.1 tei,
// 8.5), so the two can never disagree about what "local" means.
func IsLocalOrPrivateHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return true
	}
	switch host {
	case "localhost", "ip6-localhost", "ip6-loopback":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}
	for _, suffix := range []string{".local", ".localhost", ".internal", ".lan", ".intranet"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	// A single-label hostname (no dot) is not a public FQDN; treat as LAN.
	return !strings.Contains(host, ".")
}
