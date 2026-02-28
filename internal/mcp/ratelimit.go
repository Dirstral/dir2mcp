package mcp

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ipRateLimiter struct {
	mu               sync.Mutex
	buckets          map[string]*tokenBucket
	rps              float64
	burst            int
	trustedProxyNets []*net.IPNet
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
}

func newIPRateLimiter(rps float64, burst int, trustedProxies []string) *ipRateLimiter {
	return &ipRateLimiter{
		buckets:          make(map[string]*tokenBucket),
		rps:              rps,
		burst:            burst,
		trustedProxyNets: parseTrustedProxyNets(trustedProxies),
	}
}

func (l *ipRateLimiter) allow(ip string) bool {
	if l == nil || l.rps <= 0 || l.burst <= 0 {
		return true
	}

	clientIP := normalizeRateLimitIP(ip)
	if clientIP == "" || isLoopbackClientIP(clientIP) {
		return true
	}

	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, exists := l.buckets[clientIP]
	if !exists {
		l.buckets[clientIP] = &tokenBucket{
			tokens:   float64(l.burst - 1),
			lastTime: now,
		}
		return true
	}

	elapsedSeconds := now.Sub(bucket.lastTime).Seconds()
	if elapsedSeconds > 0 {
		bucket.tokens += elapsedSeconds * l.rps
		maxTokens := float64(l.burst)
		if bucket.tokens > maxTokens {
			bucket.tokens = maxTokens
		}
	}

	bucket.lastTime = now
	if bucket.tokens >= 1 {
		bucket.tokens -= 1
		return true
	}

	return false
}

func (l *ipRateLimiter) cleanup(maxAge time.Duration) {
	if l == nil || maxAge <= 0 {
		return
	}

	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	for ip, bucket := range l.buckets {
		if bucket == nil || now.Sub(bucket.lastTime) > maxAge {
			delete(l.buckets, ip)
		}
	}
}

func realIP(r *http.Request, limiter *ipRateLimiter) string {
	if r == nil {
		return ""
	}

	remoteAddr := strings.TrimSpace(r.RemoteAddr)
	peerIP := parseIPValue(remoteAddr)
	if peerIP == nil {
		return remoteAddr
	}

	peerIdentity := peerIP.String()
	if limiter != nil && limiter.isTrustedProxy(peerIdentity) {
		if xffIP := rightMostClientXFFIP(r.Header.Get("X-Forwarded-For"), limiter); xffIP != nil {
			return xffIP.String()
		}
	}

	return peerIdentity
}

func normalizeRateLimitIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}

	if strings.EqualFold(ip, "localhost") {
		return "localhost"
	}

	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}

	ip = strings.Trim(ip, "[]")
	if zoneIndex := strings.Index(ip, "%"); zoneIndex >= 0 {
		ip = ip[:zoneIndex]
	}

	if parsed := net.ParseIP(ip); parsed != nil {
		return parsed.String()
	}

	return strings.ToLower(ip)
}

func isLoopbackClientIP(ip string) bool {
	if strings.EqualFold(strings.TrimSpace(ip), "localhost") {
		return true
	}

	parsed := net.ParseIP(strings.TrimSpace(ip))
	return parsed != nil && parsed.IsLoopback()
}

func parseTrustedProxyNets(values []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(values))
	seen := make(map[string]struct{}, len(values))

	for _, value := range values {
		network := parseTrustedProxyNet(value)
		if network == nil {
			continue
		}
		key := network.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		nets = append(nets, network)
	}
	return nets
}

func parseTrustedProxyNet(value string) *net.IPNet {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.Contains(value, "/") {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil
		}
		return network
	}

	ip := net.ParseIP(value)
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
}

func (l *ipRateLimiter) isTrustedProxy(remoteIP string) bool {
	if l == nil {
		return false
	}
	ip := parseIPValue(remoteIP)
	if ip == nil {
		return false
	}
	for _, network := range l.trustedProxyNets {
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

// rightMostClientXFFIP selects the real client IP from X-Forwarded-For by
// iterating entries right-to-left and returning the first address that is not
// a trusted proxy. This prevents spoofing: an attacker can inject arbitrary
// values at the left of the header, but every trusted hop appends from the
// right, so the right-most untrusted entry is the true origin.
func rightMostClientXFFIP(xff string, limiter *ipRateLimiter) net.IP {
	xff = strings.TrimSpace(xff)
	if xff == "" {
		return nil
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := parseIPValue(parts[i])
		if ip == nil {
			continue
		}
		if limiter == nil || !limiter.isTrustedProxy(ip.String()) {
			return ip
		}
	}
	return nil
}

func parseIPValue(value string) net.IP {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	if zoneIndex := strings.Index(value, "%"); zoneIndex >= 0 {
		value = value[:zoneIndex]
	}
	return net.ParseIP(value)
}
