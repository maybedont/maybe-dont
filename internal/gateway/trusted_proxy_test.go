package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTrustedProxyChecker_EmptyConfig(t *testing.T) {
	checker := NewTrustedProxyChecker(nil)
	assert.True(t, checker.TrustAll(), "Empty config should trust all")

	checker2 := NewTrustedProxyChecker([]string{})
	assert.True(t, checker2.TrustAll(), "Empty slice should trust all")
}

func TestNewTrustedProxyChecker_WithIPv4(t *testing.T) {
	checker := NewTrustedProxyChecker([]string{"192.168.1.1", "10.0.0.1"})
	assert.False(t, checker.TrustAll())

	assert.True(t, checker.IsTrusted("192.168.1.1"))
	assert.True(t, checker.IsTrusted("10.0.0.1"))
	assert.False(t, checker.IsTrusted("192.168.1.2"))
	assert.False(t, checker.IsTrusted("8.8.8.8"))
}

func TestNewTrustedProxyChecker_WithIPv6(t *testing.T) {
	checker := NewTrustedProxyChecker([]string{"::1", "fe80::1", "2001:db8::1"})
	assert.False(t, checker.TrustAll())

	assert.True(t, checker.IsTrusted("::1"))
	assert.True(t, checker.IsTrusted("fe80::1"))
	assert.True(t, checker.IsTrusted("2001:db8::1"))
	assert.False(t, checker.IsTrusted("2001:db8::2"))
	assert.False(t, checker.IsTrusted("fe80::2"))
}

func TestNewTrustedProxyChecker_WithCIDR(t *testing.T) {
	checker := NewTrustedProxyChecker([]string{
		"10.0.0.0/8",     // Private A
		"172.16.0.0/12",  // Private B
		"192.168.0.0/16", // Private C
		"fc00::/7",       // IPv6 unique local
	})
	assert.False(t, checker.TrustAll())

	// IPv4 CIDR tests
	assert.True(t, checker.IsTrusted("10.0.0.1"))
	assert.True(t, checker.IsTrusted("10.255.255.255"))
	assert.True(t, checker.IsTrusted("172.16.0.1"))
	assert.True(t, checker.IsTrusted("172.31.255.255"))
	assert.True(t, checker.IsTrusted("192.168.0.1"))
	assert.True(t, checker.IsTrusted("192.168.255.255"))
	assert.False(t, checker.IsTrusted("8.8.8.8"))
	assert.False(t, checker.IsTrusted("172.32.0.1")) // Outside 172.16.0.0/12

	// IPv6 CIDR tests
	assert.True(t, checker.IsTrusted("fc00::1"))
	assert.True(t, checker.IsTrusted("fd00::1"))
	assert.False(t, checker.IsTrusted("2001:db8::1"))
}

func TestNewTrustedProxyChecker_MixedConfig(t *testing.T) {
	checker := NewTrustedProxyChecker([]string{
		"192.168.1.100", // Individual IPv4
		"10.0.0.0/8",    // CIDR
		"::1",           // Individual IPv6
		"fe80::/10",     // IPv6 link-local
	})
	assert.False(t, checker.TrustAll())

	assert.True(t, checker.IsTrusted("192.168.1.100"))
	assert.False(t, checker.IsTrusted("192.168.1.101"))
	assert.True(t, checker.IsTrusted("10.1.2.3"))
	assert.True(t, checker.IsTrusted("::1"))
	assert.True(t, checker.IsTrusted("fe80::1"))
}

func TestNewTrustedProxyChecker_InvalidEntries(t *testing.T) {
	// Invalid entries are silently ignored
	checker := NewTrustedProxyChecker([]string{
		"192.168.1.1",    // Valid
		"not-an-ip",      // Invalid
		"",               // Empty
		"   ",            // Whitespace only
		"192.168.1.0/33", // Invalid CIDR
	})

	// Should still work with valid entries
	assert.True(t, checker.IsTrusted("192.168.1.1"))
	assert.False(t, checker.IsTrusted("not-an-ip"))
}

func TestTrustedProxyChecker_IsTrusted_InvalidIP(t *testing.T) {
	checker := NewTrustedProxyChecker([]string{"192.168.1.0/24"})

	assert.False(t, checker.IsTrusted("not-an-ip"))
	assert.False(t, checker.IsTrusted(""))
	assert.False(t, checker.IsTrusted("  "))
}

func TestTrustedProxyChecker_ExtractClientIP_TrustAll(t *testing.T) {
	checker := NewTrustedProxyChecker(nil) // Trust all

	tests := []struct {
		name       string
		xff        string
		xRealIP    string
		remoteAddr string
		expected   string
	}{
		{
			name:       "Single IP in XFF",
			xff:        "1.2.3.4",
			xRealIP:    "",
			remoteAddr: "10.0.0.1:8080",
			expected:   "1.2.3.4",
		},
		{
			name:       "Multiple IPs in XFF - returns leftmost",
			xff:        "1.2.3.4, 10.0.0.1, 10.0.0.2",
			xRealIP:    "",
			remoteAddr: "10.0.0.3:8080",
			expected:   "1.2.3.4",
		},
		{
			name:       "Empty XFF with X-Real-IP",
			xff:        "",
			xRealIP:    "1.2.3.4",
			remoteAddr: "10.0.0.1:8080",
			expected:   "1.2.3.4",
		},
		{
			name:       "Empty XFF and X-Real-IP - falls back to RemoteAddr",
			xff:        "",
			xRealIP:    "",
			remoteAddr: "1.2.3.4:8080",
			expected:   "1.2.3.4",
		},
		{
			name:       "IPv6 RemoteAddr with brackets",
			xff:        "",
			xRealIP:    "",
			remoteAddr: "[::1]:8080",
			expected:   "::1",
		},
		{
			name:       "IPv6 in XFF",
			xff:        "2001:db8::1, ::1",
			xRealIP:    "",
			remoteAddr: "[::1]:8080",
			expected:   "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checker.ExtractClientIP(tt.xff, tt.xRealIP, tt.remoteAddr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTrustedProxyChecker_ExtractClientIP_WithTrustedProxies(t *testing.T) {
	// Configure proxies that are trusted (internal network)
	checker := NewTrustedProxyChecker([]string{
		"10.0.0.0/8",     // Internal network
		"192.168.0.0/16", // Internal network
	})

	tests := []struct {
		name       string
		xff        string
		xRealIP    string
		remoteAddr string
		expected   string
	}{
		{
			name:       "External client through trusted proxies - returns external IP",
			xff:        "1.2.3.4, 10.0.0.1, 10.0.0.2",
			xRealIP:    "",
			remoteAddr: "10.0.0.3:8080",
			expected:   "1.2.3.4", // First non-trusted IP from right
		},
		{
			name:       "All IPs are trusted - returns leftmost",
			xff:        "10.1.1.1, 10.0.0.1, 10.0.0.2",
			xRealIP:    "",
			remoteAddr: "10.0.0.3:8080",
			expected:   "10.1.1.1",
		},
		{
			name:       "External IP in middle of chain",
			xff:        "10.0.0.5, 8.8.8.8, 10.0.0.1, 10.0.0.2",
			xRealIP:    "",
			remoteAddr: "10.0.0.3:8080",
			expected:   "8.8.8.8", // First non-trusted from right
		},
		{
			name:       "Single external IP",
			xff:        "1.2.3.4",
			xRealIP:    "",
			remoteAddr: "10.0.0.1:8080",
			expected:   "1.2.3.4",
		},
		{
			name:       "Single trusted IP - returns it (leftmost)",
			xff:        "10.0.0.1",
			xRealIP:    "",
			remoteAddr: "10.0.0.2:8080",
			expected:   "10.0.0.1",
		},
		{
			name:       "Empty XFF with X-Real-IP",
			xff:        "",
			xRealIP:    "1.2.3.4",
			remoteAddr: "10.0.0.1:8080",
			expected:   "1.2.3.4",
		},
		{
			name:       "Empty headers - falls back to RemoteAddr",
			xff:        "",
			xRealIP:    "",
			remoteAddr: "10.0.0.1:8080",
			expected:   "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checker.ExtractClientIP(tt.xff, tt.xRealIP, tt.remoteAddr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTrustedProxyChecker_ExtractClientIP_IPv6(t *testing.T) {
	checker := NewTrustedProxyChecker([]string{
		"fe80::/10", // Link-local
		"::1",       // Localhost
	})

	tests := []struct {
		name       string
		xff        string
		xRealIP    string
		remoteAddr string
		expected   string
	}{
		{
			name:       "External IPv6 through trusted proxies",
			xff:        "2001:db8::1, fe80::1, ::1",
			xRealIP:    "",
			remoteAddr: "[::1]:8080",
			expected:   "2001:db8::1",
		},
		{
			name:       "All trusted IPv6",
			xff:        "fe80::1, fe80::2, ::1",
			xRealIP:    "",
			remoteAddr: "[::1]:8080",
			expected:   "fe80::1", // Leftmost since all trusted
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checker.ExtractClientIP(tt.xff, tt.xRealIP, tt.remoteAddr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTrustedProxyChecker_ExtractClientIP_EdgeCases(t *testing.T) {
	checker := NewTrustedProxyChecker([]string{"10.0.0.0/8"})

	tests := []struct {
		name       string
		xff        string
		xRealIP    string
		remoteAddr string
		expected   string
	}{
		{
			name:       "Empty XFF with whitespace",
			xff:        "   ",
			xRealIP:    "",
			remoteAddr: "1.2.3.4:8080",
			expected:   "1.2.3.4",
		},
		{
			name:       "XFF with extra commas",
			xff:        "1.2.3.4, , 10.0.0.1, ",
			xRealIP:    "",
			remoteAddr: "10.0.0.2:8080",
			expected:   "1.2.3.4",
		},
		{
			name:       "XFF with whitespace around IPs",
			xff:        "  1.2.3.4  ,  10.0.0.1  ",
			xRealIP:    "",
			remoteAddr: "10.0.0.2:8080",
			expected:   "1.2.3.4",
		},
		{
			name:       "RemoteAddr without port",
			xff:        "",
			xRealIP:    "",
			remoteAddr: "1.2.3.4",
			expected:   "1.2.3.4",
		},
		{
			name:       "X-Real-IP with whitespace",
			xff:        "",
			xRealIP:    "  1.2.3.4  ",
			remoteAddr: "10.0.0.1:8080",
			expected:   "1.2.3.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checker.ExtractClientIP(tt.xff, tt.xRealIP, tt.remoteAddr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseRemoteAddr(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{
			name:       "IPv4 with port",
			remoteAddr: "192.168.1.1:8080",
			expected:   "192.168.1.1",
		},
		{
			name:       "IPv4 without port",
			remoteAddr: "192.168.1.1",
			expected:   "192.168.1.1",
		},
		{
			name:       "IPv6 with brackets and port",
			remoteAddr: "[::1]:8080",
			expected:   "::1",
		},
		{
			name:       "IPv6 with brackets without port",
			remoteAddr: "[::1]",
			expected:   "::1", // Brackets are stripped by parseRemoteAddr
		},
		{
			name:       "Empty string",
			remoteAddr: "",
			expected:   "",
		},
		{
			name:       "Full IPv6 with brackets and port",
			remoteAddr: "[2001:db8::1]:8080",
			expected:   "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRemoteAddr(tt.remoteAddr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTrustedProxyChecker_RealWorldScenario_CloudFlare(t *testing.T) {
	// Simulate CloudFlare -> Load Balancer -> App scenario
	// CloudFlare IPs are public, we trust our load balancer
	checker := NewTrustedProxyChecker([]string{
		"10.0.0.0/8", // Internal load balancers
	})

	// Request flow: Client -> CloudFlare -> LB1 -> LB2 -> App
	// XFF: Client IP, CF IP, LB1 IP (LB2 adds itself to RemoteAddr)
	result := checker.ExtractClientIP(
		"203.0.113.50, 104.16.0.1, 10.0.0.5", // CloudFlare IP is 104.16.0.1
		"",
		"10.0.0.10:8080", // Direct connection from LB2
	)

	// Should return CloudFlare IP since we only trust internal IPs
	assert.Equal(t, "104.16.0.1", result)
}

func TestTrustedProxyChecker_RealWorldScenario_FullyTrustedChain(t *testing.T) {
	// Simulate scenario where we trust all proxies in the chain
	checker := NewTrustedProxyChecker([]string{
		"10.0.0.0/8",    // Internal
		"104.16.0.0/12", // CloudFlare
	})

	result := checker.ExtractClientIP(
		"203.0.113.50, 104.16.0.1, 10.0.0.5",
		"",
		"10.0.0.10:8080",
	)

	// Should return the original client IP
	assert.Equal(t, "203.0.113.50", result)
}

func TestNewTrustedProxyChecker_WhitespaceInConfig(t *testing.T) {
	// Test that whitespace in config entries is handled
	checker := NewTrustedProxyChecker([]string{
		"  192.168.1.1  ",
		"  10.0.0.0/8  ",
	})

	require.False(t, checker.TrustAll())
	assert.True(t, checker.IsTrusted("192.168.1.1"))
	assert.True(t, checker.IsTrusted("10.1.2.3"))
}
