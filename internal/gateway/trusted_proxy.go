package gateway

import (
	"net"
	"strings"
)

// TrustedProxyChecker checks if IP addresses are trusted proxies.
// It supports individual IPv4/IPv6 addresses and CIDR blocks.
type TrustedProxyChecker struct {
	// nets contains parsed CIDR networks
	nets []*net.IPNet
	// ips contains individual IP addresses that weren't CIDR notation
	ips map[string]bool
	// trustAll indicates if all proxies should be trusted (empty config)
	trustAll bool
}

// NewTrustedProxyChecker creates a new checker from a list of trusted proxy specifications.
// Each entry can be an IPv4 address, IPv6 address, or CIDR block.
// If the list is empty or nil, all proxies are trusted.
func NewTrustedProxyChecker(trustedProxies []string) *TrustedProxyChecker {
	checker := &TrustedProxyChecker{
		ips:      make(map[string]bool),
		trustAll: len(trustedProxies) == 0,
	}

	if checker.trustAll {
		return checker
	}

	for _, entry := range trustedProxies {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		// Try parsing as CIDR first
		_, ipNet, err := net.ParseCIDR(entry)
		if err == nil {
			checker.nets = append(checker.nets, ipNet)
			continue
		}

		// Try parsing as individual IP
		ip := net.ParseIP(entry)
		if ip != nil {
			// Normalize to string form for consistent comparison
			checker.ips[ip.String()] = true
		}
		// Invalid entries are silently ignored
	}

	return checker
}

// IsTrusted checks if the given IP address is a trusted proxy.
// If trustAll is true, returns true for any IP.
func (c *TrustedProxyChecker) IsTrusted(ipStr string) bool {
	if c.trustAll {
		return true
	}

	ipStr = strings.TrimSpace(ipStr)
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	// Check individual IPs
	if c.ips[ip.String()] {
		return true
	}

	// Check CIDR networks
	for _, ipNet := range c.nets {
		if ipNet.Contains(ip) {
			return true
		}
	}

	return false
}

// TrustAll returns true if this checker trusts all proxies (empty configuration).
func (c *TrustedProxyChecker) TrustAll() bool {
	return c.trustAll
}

// ExtractClientIP extracts the real client IP address from an HTTP request's
// X-Forwarded-For header using the trusted proxy configuration.
//
// Algorithm:
//   - If trustAll is true (empty config), return the leftmost (first) IP from X-Forwarded-For
//   - Otherwise, iterate from right to left through X-Forwarded-For IPs
//   - Return the first IP that is NOT a trusted proxy (this is the real client IP)
//   - If all IPs are trusted, return the leftmost IP
//
// Parameters:
//   - xff: The X-Forwarded-For header value (comma-separated list of IPs)
//   - remoteAddr: The direct connection's remote address (fallback)
//
// Returns the determined client IP address.
func (c *TrustedProxyChecker) ExtractClientIP(xff, xRealIP, remoteAddr string) string {
	// Parse remoteAddr to extract just the IP (remove port)
	directIP := parseRemoteAddr(remoteAddr)

	// If X-Forwarded-For is empty, fall back to X-Real-IP or direct connection
	if xff == "" {
		if xRealIP != "" {
			return strings.TrimSpace(xRealIP)
		}
		return directIP
	}

	// Split X-Forwarded-For into individual IPs
	ips := strings.Split(xff, ",")
	for i := range ips {
		ips[i] = strings.TrimSpace(ips[i])
	}

	// Remove empty entries
	var cleanIPs []string
	for _, ip := range ips {
		if ip != "" {
			cleanIPs = append(cleanIPs, ip)
		}
	}

	if len(cleanIPs) == 0 {
		if xRealIP != "" {
			return strings.TrimSpace(xRealIP)
		}
		return directIP
	}

	// If we trust all proxies, return the leftmost (first) IP
	// This is the traditional behavior
	if c.trustAll {
		return cleanIPs[0]
	}

	// Iterate from right to left to find the first non-trusted IP
	// The rightmost IP is added by the most recent proxy, working back to the original client
	for i := len(cleanIPs) - 1; i >= 0; i-- {
		ip := cleanIPs[i]
		if !c.IsTrusted(ip) {
			return ip
		}
	}

	// All IPs are trusted proxies, return the leftmost (original client according to chain)
	return cleanIPs[0]
}

// parseRemoteAddr extracts the IP address from a RemoteAddr string (host:port format)
func parseRemoteAddr(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}

	// Handle IPv6 addresses with brackets: [::1]:8080
	if strings.HasPrefix(remoteAddr, "[") {
		if bracketIdx := strings.Index(remoteAddr, "]"); bracketIdx != -1 {
			// Return the IP without brackets
			return remoteAddr[1:bracketIdx]
		}
	}

	// Try standard host:port split
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// No port, return as-is
		return remoteAddr
	}

	return host
}
