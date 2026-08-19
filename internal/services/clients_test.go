package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseRCIHotspotJSON(t *testing.T) {
	rawJSON := []byte(`{
		"host": [
			{
				"mac": "F8:4D:89:BF:CE:5B",
				"ip": "172.16.0.148",
				"hostname": "avolkov",
				"name": "Iphone 12",
				"active": true,
				"link": "up",
				"interface": {
					"name": "Home"
				}
			},
			{
				"mac": "00:2B:67:38:43:C7",
				"ip": "172.16.0.136",
				"hostname": "LEGION",
				"name": "",
				"active": true,
				"link": "up",
				"interface": {
					"name": "Home"
				}
			},
			{
				"mac": "68:28:6C:F0:70:9B",
				"ip": "0.0.0.0",
				"hostname": "",
				"name": "PS5 Pro",
				"active": false,
				"link": "down"
			}
		]
	}`)

	clients, err := ParseRCIHotspotJSON(rawJSON)
	if err != nil {
		t.Fatalf("unexpected error parsing RCI JSON: %v", err)
	}

	if len(clients) != 2 {
		t.Fatalf("expected 2 valid clients, got %d", len(clients))
	}

	iphone, ok := clients["172.16.0.148"]
	if !ok {
		t.Fatalf("expected client 172.16.0.148 to be present")
	}
	if iphone.DisplayName != "Iphone 12" {
		t.Errorf("expected DisplayName 'Iphone 12', got '%s'", iphone.DisplayName)
	}
	if iphone.MAC != "f8:4d:89:bf:ce:5b" {
		t.Errorf("expected lowercase MAC, got '%s'", iphone.MAC)
	}
	if !iphone.Active {
		t.Errorf("expected active=true")
	}

	legion, ok := clients["172.16.0.136"]
	if !ok {
		t.Fatalf("expected client 172.16.0.136 to be present")
	}
	if legion.DisplayName != "LEGION" {
		t.Errorf("expected DisplayName 'LEGION' fallback from hostname, got '%s'", legion.DisplayName)
	}

	if _, ok := clients["0.0.0.0"]; ok {
		t.Errorf("0.0.0.0 should not be indexed")
	}
}

func TestParseNdmcHotspotText(t *testing.T) {
	text := `
             host: 
                  mac: 00:2b:67:38:43:c7
                   ip: 172.16.0.136
             hostname: LEGION
                 name: LEGION
               active: yes
                 link: up

             host: 
                  mac: f8:4d:89:bf:ce:5b
                   ip: 172.16.0.148
             hostname: avolkov
                 name: Iphone 12
               active: yes
                 link: up

             host: 
                  mac: 68:28:6c:f0:70:9b
                   ip: 0.0.0.0
                 name: PS5 Pro
               active: no
                 link: down
`
	clients := ParseNdmcHotspotText(text)
	if len(clients) != 2 {
		t.Fatalf("expected 2 clients from ndmc text, got %d", len(clients))
	}

	iphone, ok := clients["172.16.0.148"]
	if !ok {
		t.Fatalf("expected client 172.16.0.148")
	}
	if iphone.DisplayName != "Iphone 12" {
		t.Errorf("expected DisplayName 'Iphone 12', got '%s'", iphone.DisplayName)
	}
	if !iphone.Active {
		t.Errorf("expected active=true")
	}
}

func TestParseArpTable(t *testing.T) {
	arpData := `IP address       HW type     Flags       HW address            Mask     Device
172.16.0.148     0x1         0x2         f8:4d:89:bf:ce:5b     *        br0
172.16.0.199     0x1         0x0         00:00:00:00:00:00     *        br0
`
	clients := ParseArpTable(arpData)
	if len(clients) != 1 {
		t.Fatalf("expected 1 valid ARP client, got %d", len(clients))
	}

	c, ok := clients["172.16.0.148"]
	if !ok {
		t.Fatalf("expected 172.16.0.148 in arp clients")
	}
	if c.MAC != "f8:4d:89:bf:ce:5b" {
		t.Errorf("expected mac f8:4d:89:bf:ce:5b, got %s", c.MAC)
	}
	if c.DisplayName != "172.16.0.148" {
		t.Errorf("expected DisplayName 172.16.0.148, got %s", c.DisplayName)
	}
}

func TestClientResolverCachingAndFallbacks(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"host": [
				{
					"mac": "f8:4d:89:bf:ce:5b",
					"ip": "172.16.0.148",
					"name": "Iphone 12",
					"active": true
				}
			]
		}`))
	}))
	defer ts.Close()

	resolver := NewClientResolver()
	resolver.SetRCIURL(ts.URL)
	resolver.SetTTL(50 * time.Millisecond)

	// First call -> hits RCI
	clients1 := resolver.GetClients()
	if len(clients1) != 1 || clients1["172.16.0.148"].DisplayName != "Iphone 12" {
		t.Fatalf("unexpected first fetch: %+v", clients1)
	}
	if callCount != 1 {
		t.Errorf("expected callCount=1, got %d", callCount)
	}

	// Second call before TTL expires -> cached
	clients2 := resolver.GetClients()
	if len(clients2) != 1 {
		t.Fatalf("unexpected cached fetch")
	}
	if callCount != 1 {
		t.Errorf("expected callCount=1 (cached), got %d", callCount)
	}

	// Test Resolve helper
	client, ok := resolver.Resolve("172.16.0.148")
	if !ok || client.DisplayName != "Iphone 12" {
		t.Errorf("Resolve failed: ok=%v, client=%+v", ok, client)
	}

	// Wait for TTL expiry and verify fallback to ndmc when RCI fails
	time.Sleep(60 * time.Millisecond)
	resolver.SetRCIURL("http://127.0.0.1:1/invalid") // Broken RCI
	resolver.execNdmc = func(ctx context.Context) ([]byte, error) {
		return []byte(`
host:
  mac: 00:2b:67:38:43:c7
  ip: 172.16.0.136
  name: LEGION
  active: yes
`), nil
	}

	clients3 := resolver.GetClients()
	if len(clients3) != 1 || clients3["172.16.0.136"].DisplayName != "LEGION" {
		t.Fatalf("fallback to ndmc failed: %+v", clients3)
	}
}

func TestClientResolverNegativeCaching(t *testing.T) {
	ndmcCalls := 0
	resolver := NewClientResolver()
	resolver.SetRCIURL("http://127.0.0.1:1/invalid")
	resolver.SetTTL(50 * time.Millisecond)
	resolver.execNdmc = func(ctx context.Context) ([]byte, error) {
		ndmcCalls++
		return nil, context.DeadlineExceeded
	}
	resolver.readArp = func() ([]byte, error) {
		return nil, context.DeadlineExceeded
	}

	// First call -> fails, but updates lastFetch
	clients1 := resolver.GetClients()
	if len(clients1) != 0 {
		t.Fatalf("expected empty map, got %v", clients1)
	}
	if ndmcCalls != 1 {
		t.Fatalf("expected ndmcCalls=1, got %d", ndmcCalls)
	}

	// Second call within TTL -> negative cached, should not call ndmc again
	clients2 := resolver.GetClients()
	if len(clients2) != 0 {
		t.Fatalf("expected empty map, got %v", clients2)
	}
	if ndmcCalls != 1 {
		t.Fatalf("expected ndmcCalls=1 (negative cached), got %d", ndmcCalls)
	}

	// Wait for TTL to expire
	time.Sleep(60 * time.Millisecond)

	// Third call after TTL -> attempts fetch again
	resolver.GetClients()
	if ndmcCalls != 2 {
		t.Fatalf("expected ndmcCalls=2 after TTL expiry, got %d", ndmcCalls)
	}
}
