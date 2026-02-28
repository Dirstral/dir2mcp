package mcp

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ipRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rps     float64
	burst   int
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
}

func newIPRateLimiter(rps float64, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		buckets: make(map[string]*tokenBucket),
		rps:     rps,
		burst:   burst,
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

func realIP(r *http.Request) string {
	if r == nil {
		return ""
	}

	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}

	remote := strings.TrimSpace(r.RemoteAddr)
	if remote == "" {
		return ""
	}

	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return remote
	}
	return host
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
