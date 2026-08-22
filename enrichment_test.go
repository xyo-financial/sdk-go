package xyo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/xyo-financial/sdk-go/v2/openapi"
)

func newTestServerAndClient(t *testing.T, expectedMethod, expectedPath string, statusCode int, responsePayload interface{}) (*httptest.Server, Client) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != expectedMethod {
			t.Errorf("expected method %q, got %q", expectedMethod, r.Method)
		}
		if r.URL.Path != expectedPath {
			t.Errorf("expected path %q, got %q", expectedPath, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("missing or invalid Authorization header: %q", r.Header.Get("Authorization"))
		}
		if expectedMethod != http.MethodGet && r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing or invalid Content-Type header: %q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Accept") != "" && !strings.Contains(r.Header.Get("Accept"), "application/json") && !strings.Contains(r.Header.Get("Accept"), "application/gzip") {
			t.Errorf("missing or invalid Accept header: %q", r.Header.Get("Accept"))
		}

		if responsePayload != nil {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(statusCode)
		if responsePayload != nil {
			_ = json.NewEncoder(w).Encode(responsePayload)
		}
	}))
	t.Cleanup(ts.Close)

	client, err := NewClient(&ClientConfig{
		APIKey:  "test-api-key",
		BaseURL: ts.URL,
	})
	if err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}

	return ts, client
}

func TestEnrichTransaction(t *testing.T) {
	reqPayload := &EnrichmentRequest{
		Content:     "Some Random Content",
		CountryCode: "GB",
	}

	t.Run("non-200 status returns error", func(t *testing.T) {
		errPayload := map[string]interface{}{
			"errors": []map[string]interface{}{
				{
					"type":     "Invalid API Key",
					"status":   http.StatusForbidden,
					"title":    "Invalid API Key",
					"instance": "InvalidClientAPIKeyException",
					"detail":   "Credits expired or an invalid API Key is given",
				},
			},
		}

		ts, client := newTestServerAndClient(t, http.MethodPost, "/v1/ai/finance/enrichment/transaction", http.StatusForbidden, errPayload)
		defer ts.Close()

		_, err := client.EnrichTransaction(context.Background(), reqPayload)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var apiErr *ErrorResponse
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected error to unwrap to *ErrorResponse, got: %v", err)
		}
		if apiErr.HTTPStatusCode != http.StatusForbidden {
			t.Errorf("expected HTTP status %d, got %d", http.StatusForbidden, apiErr.HTTPStatusCode)
		}
		if len(apiErr.Errors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(apiErr.Errors))
		}
		if apiErr.Errors[0].Title != "Invalid API Key" {
			t.Errorf("expected title %q, got %q", "Invalid API Key", apiErr.Errors[0].Title)
		}
	})

	t.Run("200 OK decodes response", func(t *testing.T) {
		payload := map[string]interface{}{
			"merchant":    "Syniol Limited",
			"description": "Software and Cloud Platform Consultancy",
			"logo":        "base64/png;31233232dsdsdaaersdasjhdsfi",
			"categories":  []string{"Tech"},
			"location":    "United Kingdom, England",
			"address":     "London, O2",
		}
		ts, client := newTestServerAndClient(t, http.MethodPost, "/v1/ai/finance/enrichment/transaction", http.StatusOK, payload)
		defer ts.Close()

		resp, err := client.EnrichTransaction(context.Background(), reqPayload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Merchant != "Syniol Limited" {
			t.Errorf("expected merchant %q, got %q", "Syniol Limited", resp.Merchant)
		}
	})
}

func TestEnrichTransactionCollection(t *testing.T) {
	requests := []*EnrichmentRequest{
		{Content: "Some Random Content", CountryCode: "GB"},
		{Content: "Some Random Content 2", CountryCode: "US"},
	}

	t.Run("non-200 status returns error", func(t *testing.T) {
		ts, client := newTestServerAndClient(t, http.MethodPost, "/v1/ai/finance/enrichment/transactions", http.StatusBadRequest, nil)
		defer ts.Close()

		_, err := client.EnrichTransactionCollection(context.Background(), requests)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("200 OK decodes response", func(t *testing.T) {
		payload := map[string]interface{}{
			"id":   "72c037df-d0d3-43ee-9470-323ff35a2e50",
			"link": "https://api.xyo.financial/ai/transactions/download/72c037df-d0d3-43ee-9470-323ff35a2e50.tar.gz",
		}
		ts, client := newTestServerAndClient(t, http.MethodPost, "/v1/ai/finance/enrichment/transactions", http.StatusOK, payload)
		defer ts.Close()

		resp, err := client.EnrichTransactionCollection(context.Background(), requests)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ID != "72c037df-d0d3-43ee-9470-323ff35a2e50" {
			t.Errorf("expected ID %q, got %q", "72c037df-d0d3-43ee-9470-323ff35a2e50", resp.ID)
		}
	})
}

func TestEnrichTransactionCollectionStatus(t *testing.T) {
	t.Run("non-200 status returns error", func(t *testing.T) {
		ts, client := newTestServerAndClient(t, http.MethodGet, "/v1/ai/finance/enrichment/status/asdsd", http.StatusBadRequest, nil)
		defer ts.Close()

		_, err := client.EnrichTransactionCollectionStatus(context.Background(), "asdsd")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("200 OK returns correct status", func(t *testing.T) {
		payload := map[string]interface{}{
			"status": EnrichmentCollectionStatusReady,
		}
		ts, client := newTestServerAndClient(t, http.MethodGet, "/v1/ai/finance/enrichment/status/72c037df-d0d3-43ee-9470-323ff35a2e50", http.StatusOK, payload)
		defer ts.Close()

		actual, err := client.EnrichTransactionCollectionStatus(context.Background(), "72c037df-d0d3-43ee-9470-323ff35a2e50")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if actual != EnrichmentCollectionStatusReady {
			t.Errorf("expected status %q, got %q", EnrichmentCollectionStatusReady, actual)
		}
	})
}

func TestDownloadEnrichmentCollection(t *testing.T) {
	t.Run("non-200 status returns error", func(t *testing.T) {
		ts, client := newTestServerAndClient(t, http.MethodGet, "/downloads/123.tar.gz", http.StatusBadRequest, nil)
		defer ts.Close()

		_, err := client.DownloadEnrichmentCollection(context.Background(), ts.URL+"/downloads/123.tar.gz")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("200 OK streams and decodes tarball", func(t *testing.T) {
		// Create an in-memory .tar.gz containing one JSON file
		var buf bytes.Buffer
		gzw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gzw)

		payload := map[string]interface{}{
			"merchant":    "Syniol Limited",
			"description": "Bulk Test",
		}
		b, _ := json.Marshal(payload)

		_ = tw.WriteHeader(&tar.Header{
			Name:     "transaction_0.json",
			Mode:     0600,
			Size:     int64(len(b)),
			Typeflag: tar.TypeReg,
		})
		_, _ = tw.Write(b)
		_ = tw.Close()
		_ = gzw.Close()

		ts, client := newTestServerAndClient(t, http.MethodGet, "/downloads/123.tar.gz", http.StatusOK, nil)
		defer ts.Close()

		// Override the test server to return our custom tarball bytes instead of JSON
		ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(buf.Bytes())
		})

		results, err := client.DownloadEnrichmentCollection(context.Background(), ts.URL+"/downloads/123.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0].Merchant != "Syniol Limited" {
			t.Errorf("expected merchant %q, got %q", "Syniol Limited", results[0].Merchant)
		}
	})
}

func TestEnrichTransactions(t *testing.T) {
	requests := []*EnrichmentRequest{
		{Content: "Costa PICKUP", CountryCode: "GB"},
		{Content: "STRBUKS GREENWICH", CountryCode: "GB"},
	}

	t.Run("200 OK decodes response", func(t *testing.T) {
		payload := map[string]interface{}{
			"id":   "batch-987",
			"link": "https://api.xyo.financial/ai/transactions/download/batch-987.tar.gz",
		}
		ts, client := newTestServerAndClient(t, http.MethodPost, "/v1/ai/finance/enrichment/transactions", http.StatusOK, payload)
		defer ts.Close()

		resp, err := client.EnrichTransactions(context.Background(), requests)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ID != "batch-987" {
			t.Errorf("expected ID %q, got %q", "batch-987", resp.ID)
		}
		if resp.Link != "https://api.xyo.financial/ai/transactions/download/batch-987.tar.gz" {
			t.Errorf("expected Link %q, got %q", "https://api.xyo.financial/ai/transactions/download/batch-987.tar.gz", resp.Link)
		}
	})
}

func TestGetEnrichmentStatus(t *testing.T) {
	t.Run("200 OK returns correct status", func(t *testing.T) {
		payload := map[string]interface{}{
			"status": EnrichmentCollectionStatusReady,
		}
		ts, client := newTestServerAndClient(t, http.MethodGet, "/v1/ai/finance/enrichment/status/batch-987", http.StatusOK, payload)
		defer ts.Close()

		actual, err := client.GetEnrichmentStatus(context.Background(), "batch-987")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if actual != EnrichmentCollectionStatusReady {
			t.Errorf("expected status %q, got %q", EnrichmentCollectionStatusReady, actual)
		}
	})
}

func TestEnrichTransaction_NilRequest(t *testing.T) {
	_, client := newTestServerAndClient(t, http.MethodPost, "/v1/ai/finance/enrichment/transaction", http.StatusOK, nil)
	_, err := client.EnrichTransaction(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
}

func TestEnrichTransactions_NilRequestInSlice(t *testing.T) {
	_, client := newTestServerAndClient(t, http.MethodPost, "/v1/ai/finance/enrichment/transactions", http.StatusOK, nil)
	reqs := []*EnrichmentRequest{
		{Content: "Valid", CountryCode: "GB"},
		nil,
	}
	_, err := client.EnrichTransactions(context.Background(), reqs)
	if err == nil {
		t.Fatal("expected error for slice containing nil request, got nil")
	}
}

func TestDownloadEnrichmentCollection_RFC7807Error(t *testing.T) {
	errPayload := map[string]interface{}{
		"errors": []map[string]interface{}{
			{
				"type":   "https://api.xyo.financial/errors/download-expired",
				"status": http.StatusGone,
				"title":  "Download Link Expired",
				"detail": "The requested batch download link has expired.",
			},
		},
	}
	ts, client := newTestServerAndClient(t, http.MethodGet, "/downloads/expired.tar.gz", http.StatusGone, errPayload)
	defer ts.Close()

	_, err := client.DownloadEnrichmentCollection(context.Background(), ts.URL+"/downloads/expired.tar.gz")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected error to unwrap to *ErrorResponse, got %v", err)
	}
	if apiErr.HTTPStatusCode != http.StatusGone {
		t.Errorf("expected HTTP status %d, got %d", http.StatusGone, apiErr.HTTPStatusCode)
	}
	if len(apiErr.Errors) != 1 || apiErr.Errors[0].Title != "Download Link Expired" {
		t.Errorf("unexpected error payload: %+v", apiErr.Errors)
	}
}

func TestDownloadEnrichmentCollection_ExternalHostNoAuthLeak(t *testing.T) {
	var capturedAuth string
	customTransport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedAuth = req.Header.Get("Authorization")
		var buf bytes.Buffer
		gzw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gzw)
		_ = tw.Close()
		_ = gzw.Close()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(buf.Bytes())),
			Header:     http.Header{"Content-Type": []string{"application/gzip"}},
		}, nil
	})

	client, err := NewClient(&ClientConfig{
		APIKey:               "secret-token",
		BaseURL:              "https://api.xyo.financial",
		HTTPClient:           &http.Client{Transport: customTransport},
		TrustedDownloadHosts: []string{"xyo-enrichment-results.s3.amazonaws.com"},
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// 1. Download from S3 domain succeeds and does NOT leak Authorization header
	results, err := client.DownloadEnrichmentCollection(context.Background(), "https://xyo-enrichment-results.s3.amazonaws.com/results.tar.gz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
	if capturedAuth != "" {
		t.Errorf("expected NO Authorization header for S3 download, got %q", capturedAuth)
	}

	// 2. Download from API base URL host attaches Authorization header
	_, err = client.DownloadEnrichmentCollection(context.Background(), "https://api.xyo.financial/downloads/results.tar.gz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedAuth != "Bearer secret-token" {
		t.Errorf("expected Authorization header for API base URL host, got %q", capturedAuth)
	}

	// 3. Untrusted rogue domain is rejected
	_, err = client.DownloadEnrichmentCollection(context.Background(), "https://evil-untrusted-domain.com/data.tar.gz")
	if err == nil {
		t.Fatal("expected error for untrusted domain download, got nil")
	}
	if !strings.Contains(err.Error(), "not permitted for secure archive downloads") {
		t.Errorf("expected 'not permitted' error, got %v", err)
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewClient_ConfigAlias(t *testing.T) {
	c, err := NewClient(&Config{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to create client with Config alias: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestGetEnrichmentStatus_EmptyID(t *testing.T) {
	_, client := newTestServerAndClient(t, http.MethodGet, "/v1/ai/finance/enrichment/status/test", http.StatusOK, nil)
	_, err := client.GetEnrichmentStatus(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id, got nil")
	}
}

func TestDownloadEnrichmentCollection_EmptyURL(t *testing.T) {
	_, client := newTestServerAndClient(t, http.MethodGet, "/downloads/123.tar.gz", http.StatusOK, nil)
	_, err := client.DownloadEnrichmentCollection(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty download URL, got nil")
	}
}

func TestDownloadEnrichmentCollection_ExceedsMaxEntrySize(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		gzw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gzw)

		// Create header claiming size exceeds DefaultMaxEntryBytes
		_ = tw.WriteHeader(&tar.Header{
			Name:     "bomb.json",
			Mode:     0600,
			Size:     DefaultMaxEntryBytes + 1024,
			Typeflag: tar.TypeReg,
		})
		_ = tw.Close()
		_ = gzw.Close()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}))
	defer ts.Close()

	client, err := NewClient(&ClientConfig{
		APIKey:  "test-api-key",
		BaseURL: ts.URL,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = client.DownloadEnrichmentCollection(context.Background(), ts.URL+"/bomb.tar.gz")
	if err == nil {
		t.Fatal("expected error when entry size exceeds limit, got nil")
	}
}

func TestVersionConstants(t *testing.T) {
	if Version == "" {
		t.Error("expected non-empty Version")
	}
	if DefaultUserAgent != "xyo-sdk-go/"+Version {
		t.Errorf("expected DefaultUserAgent 'xyo-sdk-go/%s', got %q", Version, DefaultUserAgent)
	}
}

func TestDownloadEnrichmentCollection_InvalidScheme(t *testing.T) {
	client, err := NewClient(&ClientConfig{
		APIKey: "test-api-key",
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	invalidURLs := []string{
		"file:///etc/passwd",
		"ftp://example.com/file.tar.gz",
		"gopher://example.com",
		"javascript:alert(1)",
	}

	for _, u := range invalidURLs {
		t.Run(u, func(t *testing.T) {
			_, err := client.DownloadEnrichmentCollection(context.Background(), u)
			if err == nil {
				t.Fatalf("expected error for invalid scheme %q, got nil", u)
			}
		})
	}
}

func TestEnrichTransaction_ForwardCompatibilityUnknownFields(t *testing.T) {
	// Simulate the backend adding new unknown fields in a future API release
	payload := map[string]interface{}{
		"merchant":               "Syniol Limited",
		"description":            "Software Consultancy",
		"categories":             []string{"Tech"},
		"logo":                   "base64/png",
		"location":               "London, UK",
		"address":                "123 Street",
		"future_additive_field":  "should_not_break",
		"another_additive_field": 999,
	}

	_, client := newTestServerAndClient(t, http.MethodPost, "/v1/ai/finance/enrichment/transaction", http.StatusOK, payload)

	resp, err := client.EnrichTransaction(context.Background(), &EnrichmentRequest{
		Content:     "Syniol Test",
		CountryCode: "GB",
	})
	if err != nil {
		t.Fatalf("expected decode to succeed with unknown fields, got error: %v", err)
	}
	if resp.Merchant != "Syniol Limited" {
		t.Errorf("expected merchant %q, got %q", "Syniol Limited", resp.Merchant)
	}
}

func TestDefaultEnterpriseTransport(t *testing.T) {
	if DefaultEnterpriseTransport == nil {
		t.Fatal("expected DefaultEnterpriseTransport to not be nil")
	}
	if DefaultEnterpriseTransport.MaxIdleConnsPerHost < 100 {
		t.Errorf("expected MaxIdleConnsPerHost >= 100, got %d", DefaultEnterpriseTransport.MaxIdleConnsPerHost)
	}
	if !DefaultEnterpriseTransport.ForceAttemptHTTP2 {
		t.Error("expected ForceAttemptHTTP2 to be true")
	}
}

func TestNewDefaultHTTPClient(t *testing.T) {
	client := NewDefaultHTTPClient()
	if client == nil {
		t.Fatal("expected NewDefaultHTTPClient to return non-nil client")
	}
	if client.Timeout != defaultTimeout {
		t.Errorf("expected timeout %v, got %v", defaultTimeout, client.Timeout)
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected transport to be *http.Transport, got %T", client.Transport)
	}
	if tr.MaxIdleConnsPerHost < 100 {
		t.Errorf("expected MaxIdleConnsPerHost >= 100, got %d", tr.MaxIdleConnsPerHost)
	}
}

func TestEnrichTransaction_InputValidation(t *testing.T) {
	_, client := newTestServerAndClient(t, http.MethodPost, "/v1/ai/finance/enrichment/transaction", http.StatusOK, nil)

	t.Run("empty content", func(t *testing.T) {
		_, err := client.EnrichTransaction(context.Background(), &EnrichmentRequest{
			Content:     "   ",
			CountryCode: "GB",
		})
		if err == nil {
			t.Fatal("expected error for empty content, got nil")
		}
	})

	t.Run("empty country code", func(t *testing.T) {
		_, err := client.EnrichTransaction(context.Background(), &EnrichmentRequest{
			Content:     "Coffee",
			CountryCode: "  ",
		})
		if err == nil {
			t.Fatal("expected error for empty country code, got nil")
		}
	})
}

func TestEnrichTransactions_InputValidation(t *testing.T) {
	_, client := newTestServerAndClient(t, http.MethodPost, "/v1/ai/finance/enrichment/transactions", http.StatusOK, nil)

	t.Run("empty reqs slice", func(t *testing.T) {
		_, err := client.EnrichTransactions(context.Background(), []*EnrichmentRequest{})
		if err == nil {
			t.Fatal("expected error for empty reqs slice, got nil")
		}
	})

	t.Run("empty content in slice", func(t *testing.T) {
		_, err := client.EnrichTransactions(context.Background(), []*EnrichmentRequest{
			{Content: "", CountryCode: "GB"},
		})
		if err == nil {
			t.Fatal("expected error for empty content in slice, got nil")
		}
	})

	t.Run("empty country code in slice", func(t *testing.T) {
		_, err := client.EnrichTransactions(context.Background(), []*EnrichmentRequest{
			{Content: "Valid", CountryCode: ""},
		})
		if err == nil {
			t.Fatal("expected error for empty country code in slice, got nil")
		}
	})
}

func TestDynamicApiKeyRotation(t *testing.T) {
	currentKey := "initial-secret-1"
	var observedAuthHeaders []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedAuthHeaders = append(observedAuthHeaders, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"merchant":    "Starbucks",
			"description": "Coffee",
			"categories":  []string{"Food"},
			"logo":        "https://example.com/logo.png",
			"location":    "Seattle",
			"address":     "2401 Utah Ave",
		})
	}))
	t.Cleanup(ts.Close)

	client, err := NewClient(&ClientConfig{
		APIKeySupplier: func() string { return currentKey },
		BaseURL:        ts.URL,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	_, err = client.EnrichTransaction(context.Background(), &EnrichmentRequest{
		Content:     "Coffee purchase",
		CountryCode: "US",
	})
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Rotate key at runtime
	currentKey = "rotated-secret-2"
	_, err = client.EnrichTransaction(context.Background(), &EnrichmentRequest{
		Content:     "Coffee purchase 2",
		CountryCode: "US",
	})
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if len(observedAuthHeaders) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(observedAuthHeaders))
	}
	if observedAuthHeaders[0] != "Bearer initial-secret-1" {
		t.Errorf("expected Bearer initial-secret-1, got %q", observedAuthHeaders[0])
	}
	if observedAuthHeaders[1] != "Bearer rotated-secret-2" {
		t.Errorf("expected Bearer rotated-secret-2, got %q", observedAuthHeaders[1])
	}
}

func TestDownloadEnrichmentCollection_UnexpectedContentType_WAF(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body><h1>Cloudflare / WAF Security Challenge</h1></body></html>"))
	}))
	t.Cleanup(ts.Close)

	client, err := NewClient(&ClientConfig{
		APIKey:  "test-api-key",
		BaseURL: ts.URL,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	_, err = client.DownloadEnrichmentCollection(context.Background(), ts.URL+"/download")
	if err == nil {
		t.Fatal("expected error for WAF html response, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected Content-Type") {
		t.Errorf("expected unexpected Content-Type error, got %v", err)
	}
}

func TestClient_Close(t *testing.T) {
	client, err := NewClient(&ClientConfig{
		APIKey: "test-api-key",
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("client.Close() failed: %v", err)
	}
}

func TestEnrichmentRequest_Validate_UTF8MultiByte(t *testing.T) {
	// 120 characters with multi-byte symbols (takes >130 bytes, but <= 128 runes)
	multiByteContent := "Café de Paris £100 €50 ¥1000 — Payment description with international currency symbols and accents ☕"
	req := &EnrichmentRequest{
		Content:     multiByteContent,
		CountryCode: "FR",
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("expected valid multi-byte request, got: %v", err)
	}

	// 129 runes should fail
	tooLongRunes := strings.Repeat("€", 129)
	reqTooLong := &EnrichmentRequest{
		Content:     tooLongRunes,
		CountryCode: "FR",
	}
	if err := reqTooLong.Validate(); err == nil {
		t.Fatal("expected error for 129 runes, got nil")
	}
}

func TestMultiTenant_OptionsAndCRLFRejection(t *testing.T) {
	var capturedUserHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUserHeader = r.Header.Get("x-api-user")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"job-tenant-123","link":"https://api.xyo.financial/downloads/job.tar.gz"}`))
		} else {
			_, _ = w.Write([]byte(`{"status":"READY"}`))
		}
	}))
	t.Cleanup(ts.Close)

	client, err := NewClient(&ClientConfig{
		APIKey:  "test-api-key",
		BaseURL: ts.URL,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	// 1. EnrichTransactionsWithOptions sends x-api-user header
	_, err = client.EnrichTransactionsWithOptions(context.Background(), []*EnrichmentRequest{
		{Content: "Uber Ride", CountryCode: "US"},
	}, &BulkEnrichmentOptions{APIUser: "jpmc-retail-dept-12"})
	if err != nil {
		t.Fatalf("EnrichTransactionsWithOptions failed: %v", err)
	}
	if capturedUserHeader != "jpmc-retail-dept-12" {
		t.Errorf("expected x-api-user header 'jpmc-retail-dept-12', got %q", capturedUserHeader)
	}

	// 2. GetEnrichmentStatusWithOptions sends x-api-user header
	capturedUserHeader = ""
	status, err := client.GetEnrichmentStatusWithOptions(context.Background(), "job-tenant-123", &BulkEnrichmentOptions{APIUser: "jpmc-retail-dept-12"})
	if err != nil {
		t.Fatalf("GetEnrichmentStatusWithOptions failed: %v", err)
	}
	if status != EnrichmentCollectionStatusReady {
		t.Errorf("expected READY status, got %v", status)
	}
	if capturedUserHeader != "jpmc-retail-dept-12" {
		t.Errorf("expected x-api-user header 'jpmc-retail-dept-12', got %q", capturedUserHeader)
	}

	// 3. CRLF rejection
	_, err = client.EnrichTransactionsWithOptions(context.Background(), []*EnrichmentRequest{
		{Content: "Uber Ride", CountryCode: "US"},
	}, &BulkEnrichmentOptions{APIUser: "user\r\ninjected-header: bad"})
	if err == nil || !strings.Contains(err.Error(), "invalid CRLF") {
		t.Errorf("expected CRLF error, got: %v", err)
	}

	_, err = client.GetEnrichmentStatusWithOptions(context.Background(), "job-123", &BulkEnrichmentOptions{APIUser: "user\ninjected: bad"})
	if err == nil || !strings.Contains(err.Error(), "invalid CRLF") {
		t.Errorf("expected CRLF error, got: %v", err)
	}
}

func TestDistributedTracing_OptionsAndContext(t *testing.T) {
	var capturedCorrelationID, capturedTraceparent string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCorrelationID = r.Header.Get("X-Correlation-ID")
		capturedTraceparent = r.Header.Get("traceparent")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"merchant":"Starbucks","description":"Coffee","categories":["Food"],"logo":"","location":"London","address":""}`))
		}
	}))
	t.Cleanup(ts.Close)

	client, err := NewClient(&ClientConfig{
		APIKey:  "test-api-key",
		BaseURL: ts.URL,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	t.Run("tracing via RequestOptions", func(t *testing.T) {
		capturedCorrelationID, capturedTraceparent = "", ""
		_, err := client.EnrichTransactionWithOptions(context.Background(), &EnrichmentRequest{
			Content:     "Coffee",
			CountryCode: "GB",
		}, &RequestOptions{
			CorrelationID: "cid-123-xyz",
			Traceparent:   "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedCorrelationID != "cid-123-xyz" {
			t.Errorf("expected X-Correlation-ID 'cid-123-xyz', got %q", capturedCorrelationID)
		}
		if capturedTraceparent != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
			t.Errorf("expected traceparent '00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01', got %q", capturedTraceparent)
		}
	})

	t.Run("tracing via context helpers", func(t *testing.T) {
		capturedCorrelationID, capturedTraceparent = "", ""
		ctx := WithCorrelationID(context.Background(), "cid-ctx-456")
		ctx = WithTraceparent(ctx, "00-1234567890abcdef1234567890abcdef-1234567890abcdef-00")

		_, err := client.EnrichTransaction(ctx, &EnrichmentRequest{
			Content:     "Coffee",
			CountryCode: "GB",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if capturedCorrelationID != "cid-ctx-456" {
			t.Errorf("expected X-Correlation-ID 'cid-ctx-456', got %q", capturedCorrelationID)
		}
		if capturedTraceparent != "00-1234567890abcdef1234567890abcdef-1234567890abcdef-00" {
			t.Errorf("expected traceparent '00-1234567890abcdef1234567890abcdef-1234567890abcdef-00', got %q", capturedTraceparent)
		}
	})

	t.Run("CRLF injection protection in tracing headers", func(t *testing.T) {
		_, err := client.EnrichTransactionWithOptions(context.Background(), &EnrichmentRequest{
			Content:     "Coffee",
			CountryCode: "GB",
		}, &RequestOptions{
			CorrelationID: "cid-123\r\nInjected-Header: bad",
		})
		if err == nil || !strings.Contains(err.Error(), "invalid CRLF") {
			t.Errorf("expected CRLF error for CorrelationID, got %v", err)
		}

		_, err = client.EnrichTransactionWithOptions(context.Background(), &EnrichmentRequest{
			Content:     "Coffee",
			CountryCode: "GB",
		}, &RequestOptions{
			Traceparent: "tp-123\nInjected-Header: bad",
		})
		if err == nil || !strings.Contains(err.Error(), "invalid CRLF") {
			t.Errorf("expected CRLF error for Traceparent, got %v", err)
		}
	})
}

func TestRateLimitErrorHandling(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.Header().Set("RateLimit-Limit", "100")
		w.Header().Set("RateLimit-Remaining", "0")
		w.Header().Set("RateLimit-Reset", "1690000000")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{
			"errors": [{
				"type": "https://api.xyo.financial/errors/rate-limit-exceeded",
				"title": "Rate Limit Exceeded",
				"status": 429,
				"detail": "Quota exhausted. Try again later."
			}]
		}`))
	}))
	t.Cleanup(ts.Close)

	client, err := NewClient(&ClientConfig{
		APIKey:  "test-api-key",
		BaseURL: ts.URL,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	_, err = client.EnrichTransaction(context.Background(), &EnrichmentRequest{
		Content:     "Coffee",
		CountryCode: "GB",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected error to unwrap to *ErrorResponse, got: %v", err)
	}

	if apiErr.HTTPStatusCode != http.StatusTooManyRequests {
		t.Errorf("expected HTTP status 429, got %d", apiErr.HTTPStatusCode)
	}
	if apiErr.RetryAfter != 30 {
		t.Errorf("expected RetryAfter 30, got %d", apiErr.RetryAfter)
	}
	if apiErr.RateLimitLimit != 100 {
		t.Errorf("expected RateLimitLimit 100, got %d", apiErr.RateLimitLimit)
	}
	if apiErr.RateLimitRemaining != 0 {
		t.Errorf("expected RateLimitRemaining 0, got %d", apiErr.RateLimitRemaining)
	}
	if apiErr.RateLimitReset != 1690000000 {
		t.Errorf("expected RateLimitReset 1690000000, got %d", apiErr.RateLimitReset)
	}
}

func TestEnrichTransactions_BatchBoundsValidation(t *testing.T) {
	client, err := NewClient(&ClientConfig{
		APIKey: "test-api-key",
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	t.Run("empty slice (0 items)", func(t *testing.T) {
		_, err := client.EnrichTransactions(context.Background(), []*EnrichmentRequest{})
		if err == nil {
			t.Fatal("expected error for empty slice, got nil")
		}
		if !strings.Contains(err.Error(), "cannot be empty") {
			t.Errorf("expected empty error message, got %v", err)
		}
	})

	t.Run("exceeds 50,000 items (50,001 items)", func(t *testing.T) {
		reqs := make([]*EnrichmentRequest, 50001)
		req := &EnrichmentRequest{Content: "Coffee", CountryCode: "GB"}
		for i := range reqs {
			reqs[i] = req
		}

		_, err := client.EnrichTransactions(context.Background(), reqs)
		if err == nil {
			t.Fatal("expected error for >50000 items, got nil")
		}
		if !strings.Contains(err.Error(), "exceeds maximum allowed length of 50,000 items") {
			t.Errorf("expected exceeds maximum limit error, got %v", err)
		}
	})
}

func TestCredentialRedactionInDebugLogs(t *testing.T) {
	var buf bytes.Buffer
	logWriter := &buf
	log.SetOutput(logWriter)
	defer log.SetOutput(os.Stderr)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"merchant":"TestMerchant","description":"TestDesc","categories":["Test"],"logo":"","location":"","address":""}`))
	}))
	defer ts.Close()

	c, err := NewClient(&ClientConfig{
		APIKey:  "super-secret-api-key-12345",
		BaseURL: ts.URL,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	cli := c.(*client)
	cli.apiClient.GetConfig().Debug = true

	ctx := context.WithValue(context.Background(), openapi.ContextAccessToken, "super-secret-api-key-12345")
	_, err = c.EnrichTransaction(ctx, &EnrichmentRequest{
		Content:     "Test Content",
		CountryCode: "US",
	})
	if err != nil {
		t.Fatalf("EnrichTransaction failed: %v", err)
	}

	logOutput := buf.String()
	if strings.Contains(logOutput, "super-secret-api-key-12345") {
		t.Errorf("log output leaked plain-text credentials! Output: %s", logOutput)
	}
	if !strings.Contains(logOutput, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in debug log output, got: %s", logOutput)
	}
}

func TestUntypedStringContextKeysIgnored(t *testing.T) {
	ts, _ := newTestServerAndClient(t, http.MethodPost, "/v1/ai/finance/enrichment/transaction", http.StatusOK, map[string]interface{}{
		"merchant":    "Test",
		"description": "Test",
	})
	defer ts.Close()

	type untypedKey string
	ctx := context.WithValue(context.Background(), untypedKey("X-Correlation-ID"), "untyped-cid")
	ctx = context.WithValue(ctx, untypedKey("traceparent"), "untyped-tp")

	cid, err := extractCorrelationID(ctx, nil)
	if err != nil {
		t.Fatalf("extractCorrelationID error: %v", err)
	}
	if cid != "" {
		t.Errorf("expected empty correlation ID from untyped string context key, got %q", cid)
	}

	tp, err := extractTraceparent(ctx, nil)
	if err != nil {
		t.Fatalf("extractTraceparent error: %v", err)
	}
	if tp != "" {
		t.Errorf("expected empty traceparent from untyped string context key, got %q", tp)
	}
}
