package resourcelibrary

import (
	"context"
	"net"
	"testing"
)

func TestPackageDNSOverride_ExplicitPublicServerOnly(t *testing.T) {
	for _, value := range []string{"", "localhost", "127.0.0.1", "169.254.169.254", "10.0.0.53", "198.18.33.154", "1.1.1.1:53", "example.com", "::1", "2001:db8::1"} {
		if _, err := packageDNSServerIP(value); err == nil {
			t.Fatalf("accepted unsafe DNS override %q", value)
		}
	}
	for _, value := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if _, err := packageDNSServerIP(value); err != nil {
			t.Fatalf("rejected public DNS server %q: %v", value, err)
		}
	}
}
func TestPackageDNSOverride_NeverReturnsFabricatedPublicAddress(t *testing.T) {
	t.Setenv(packageDNSServerEnv, "198.18.33.154")
	_, err := packageLookup(context.Background(), "dev.nexusdock.co")
	if err == nil {
		t.Fatal("fake-IP DNS override was accepted")
	}
	ips, err := packageLookup(context.Background(), "127.0.0.1")
	if err != nil || len(ips) != 1 || !ips[0].Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("literal host result %v %v", ips, err)
	}
	if publicIP(ips[0]) {
		t.Fatal("localhost unexpectedly became public")
	}

	t.Setenv(packageDNSServerEnv, "localhost")
	if _, err := AuthorizeCloudDownload("https://dev.nexusdock.co", "https://dev.nexusdock.co/pkg.zip"); err == nil {
		t.Fatal("invalid alternate DNS was not rejected")
	}
}
