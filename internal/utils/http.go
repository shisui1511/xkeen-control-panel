package utils

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SafeHTTPClient returns an http.Client that is protected against SSRF.
// It prevents connections to private, loopback, and link-local IP addresses.
func SafeHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}

				ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
				if err != nil {
					// Fallback to public resolver if system DNS resolution fails
					r := &net.Resolver{
						PreferGo: true,
						Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
							d := net.Dialer{
								Timeout: 3 * time.Second,
							}
							return d.DialContext(ctx, "udp", "1.1.1.1:53")
						},
					}
					var err2 error
					ips, err2 = r.LookupIP(ctx, "ip", host)
					if err2 != nil {
						return nil, fmt.Errorf("DNS lookup failed: system resolver error: %v, fallback resolver error: %v", err, err2)
					}
				}

				var chosenIP net.IP
				for _, ip := range ips {
					if isPrivateIP(ip) {
						continue
					}
					chosenIP = ip
					break
				}

				if chosenIP == nil {
					return nil, fmt.Errorf("access to private network is prohibited")
				}

				// Dial the specific IP to avoid TOCTOU
				return (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext(ctx, network, net.JoinHostPort(chosenIP.String(), port))
			},
		},
	}
}

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return true
	}
	// Check CGNAT (100.64.0.0/10)
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 100 && (ip4[1] >= 64 && ip4[1] <= 127) {
			return true
		}
	}
	return false
}

// ValidateURL checks if the given URL is safe from SSRF by ensuring it is a HTTP/HTTPS URL
// and does not point to a loopback, link-local, or private IP address.
// If allowPrivate is true, private and loopback IP addresses are allowed (useful for local tests).
func ValidateURL(ctx context.Context, rawURL string, allowPrivate bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty host")
	}

	if allowPrivate {
		return nil
	}

	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("SSRF: target is a private/loopback IP address")
		}
		return nil
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		// DNS lookup failure is allowed; the actual request will fail later.
		return nil
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("SSRF: target resolves to a private/loopback IP address")
		}
	}
	return nil
}
