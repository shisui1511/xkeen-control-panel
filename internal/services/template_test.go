package services

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"testing"
)

type mockRoundTripper struct {
	roundTrip func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTrip(req)
}

func TestTemplateService_List(t *testing.T) {
	svc := NewTemplateService()
	list := svc.List()
	if len(list) == 0 {
		t.Fatal("expected at least one template")
	}
	if list[0].Name == "" || list[0].URL == "" {
		t.Errorf("template list contains invalid templates: %+v", list[0])
	}
}

func TestTemplateService_FetchByName(t *testing.T) {
	svc := NewTemplateService()

	// Test invalid template name
	_, err := svc.FetchByName("Non-existent Template Name")
	if err == nil {
		t.Error("expected error for non-existent template, got nil")
	}

	// Hybrid check for network connection: lookup raw.githubusercontent.com
	_, err = net.LookupIP("raw.githubusercontent.com")
	if err != nil {
		t.Skip("skipping network template fetch test: no internet connection")
		return
	}

	// If internet is connected, test fetching default template
	content, err := svc.FetchByName("Xray: VLESS + Reality")
	if err != nil {
		t.Fatalf("failed to fetch template with network: %v", err)
	}
	if content == "" {
		t.Error("expected non-empty template content")
	}
}

func TestTemplateService_FetchByName_Mocked(t *testing.T) {
	svc := NewTemplateService()

	_, err := net.LookupIP("raw.githubusercontent.com")
	if err != nil {
		t.Skip("skipping mocked template fetch test: no internet connection for DNS lookup")
		return
	}

	mockContent := "mock config content"
	svc.httpClient = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString(mockContent)),
					Header:     make(http.Header),
				}, nil
			},
		},
	}

	content, err := svc.FetchByName("Xray: VLESS + Reality")
	if err != nil {
		t.Fatalf("failed to fetch template: %v", err)
	}
	if content != mockContent {
		t.Errorf("expected content %q, got %q", mockContent, content)
	}
}
