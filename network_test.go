package decruft

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestIsBlockedAddr(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		{ip: "0.0.0.0", blocked: true},
		{ip: "10.0.0.1", blocked: true},
		{ip: "100.64.0.1", blocked: true},
		{ip: "127.0.0.1", blocked: true},
		{ip: "169.254.169.254", blocked: true},
		{ip: "172.16.0.1", blocked: true},
		{ip: "192.0.0.1", blocked: true},
		{ip: "192.0.2.1", blocked: true},
		{ip: "192.168.0.1", blocked: true},
		{ip: "198.18.0.1", blocked: true},
		{ip: "198.51.100.1", blocked: true},
		{ip: "203.0.113.1", blocked: true},
		{ip: "224.0.0.1", blocked: true},
		{ip: "240.0.0.1", blocked: true},
		{ip: "255.255.255.255", blocked: true},
		{ip: "::", blocked: true},
		{ip: "::1", blocked: true},
		{ip: "::ffff:127.0.0.1", blocked: true},
		{ip: "64:ff9b::1", blocked: true},
		{ip: "100::1", blocked: true},
		{ip: "2001:db8::1", blocked: true},
		{ip: "fc00::1", blocked: true},
		{ip: "fe80::1", blocked: true},
		{ip: "ff00::1", blocked: true},
		{ip: "1.1.1.1"},
		{ip: "8.8.8.8"},
		{ip: "100.63.255.255"},
		{ip: "100.128.0.0"},
		{ip: "172.15.255.255"},
		{ip: "172.32.0.0"},
		{ip: "192.1.255.255"},
		{ip: "192.169.0.0"},
		{ip: "198.17.255.255"},
		{ip: "198.20.0.0"},
		{ip: "2001:4860:4860::8888"},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			got := IsBlockedAddr(netip.MustParseAddr(tt.ip))
			if got != tt.blocked {
				t.Fatalf("IsBlockedAddr(%s): got %t, want %t", tt.ip, got, tt.blocked)
			}
		})
	}
}

func TestSafeDialContextRejectsMalformedAddress(t *testing.T) {
	_, err := safeDialContext(context.Background(), "tcp", "missing-port")
	if err == nil || !strings.Contains(err.Error(), "bad address") {
		t.Fatalf("error: got %v", err)
	}
}

func TestSafeDialContextBlocksReservedLiterals(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1:80",
		"10.0.0.1:443",
		"[::1]:80",
		"[::ffff:127.0.0.1]:80",
	} {
		t.Run(addr, func(t *testing.T) {
			_, err := safeDialContext(context.Background(), "tcp", addr)
			if err == nil || !strings.Contains(err.Error(), "reserved address blocked") {
				t.Fatalf("error: got %v", err)
			}
		})
	}
}

func TestSafeDialContextHonorsCanceledContextForPublicLiteral(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := safeDialContext(ctx, "tcp", "8.8.8.8:80")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error: got %v, want context canceled", err)
	}
}

func TestSafeDialContextReportsDNSFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := safeDialContext(ctx, "tcp", "unresolvable.invalid:80")
	if err == nil || !strings.Contains(err.Error(), "could not resolve") {
		t.Fatalf("error: got %v", err)
	}
}
