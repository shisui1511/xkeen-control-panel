package utils

import (
	"context"
	"fmt"
	"net"
	"net/http"
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
					return nil, err
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
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate()
}
