package xyo

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/xyo-financial/sdk-go/v2/openapi"
)

// EnrichmentRequest is the request payload for single and bulk transaction enrichment.
type EnrichmentRequest struct {
	// Content is the payment description, maximum 128 characters.
	Content string `json:"content"`
	// CountryCode is the ISO 3166-1 alpha-2 two-character country code.
	CountryCode string `json:"countryCode"`
}

// RequestOptions specifies optional multi-tenant or distributed tracing headers.
type RequestOptions struct {
	// APIUser is the tenant or user ID passed in the x-api-user HTTP header (e.g. for multi-tenant metering).
	APIUser string
	// CorrelationID is the unique caller correlation identifier passed in the X-Correlation-ID HTTP header.
	CorrelationID string
	// Traceparent is the standard W3C TraceContext header passed in the traceparent HTTP header.
	Traceparent string
}

// BulkEnrichmentOptions is an alias for RequestOptions retained for backward compatibility.
type BulkEnrichmentOptions = RequestOptions

type contextKey string

const (
	correlationIDContextKey contextKey = "xyo-correlation-id"
	traceparentContextKey   contextKey = "xyo-traceparent"
)

// WithCorrelationID returns a context containing the specified correlation ID for distributed tracing.
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, correlationIDContextKey, correlationID)
}

// WithTraceparent returns a context containing the specified W3C traceparent header for distributed tracing.
func WithTraceparent(ctx context.Context, traceparent string) context.Context {
	return context.WithValue(ctx, traceparentContextKey, traceparent)
}

// extractHeaderValue resolves a header value from opts (direct) or ctx (via key),
// then rejects any value containing CRLF characters to prevent header injection.
func extractHeaderValue(ctx context.Context, direct string, ctxKey any, errLabel string) (string, error) {
	val := direct
	if val == "" && ctx != nil {
		if v, ok := ctx.Value(ctxKey).(string); ok {
			val = v
		}
	}
	if strings.ContainsAny(val, "\r\n") {
		return "", fmt.Errorf("%s contains invalid CRLF characters", errLabel)
	}
	return val, nil
}

func extractCorrelationID(ctx context.Context, opts *RequestOptions) (string, error) {
	var direct string
	if opts != nil {
		direct = opts.CorrelationID
	}
	return extractHeaderValue(ctx, direct, correlationIDContextKey, "correlationID")
}

func extractTraceparent(ctx context.Context, opts *RequestOptions) (string, error) {
	var direct string
	if opts != nil {
		direct = opts.Traceparent
	}
	return extractHeaderValue(ctx, direct, traceparentContextKey, "traceparent")
}

// EnrichmentResponse is the result of a single payment transaction enrichment.
type EnrichmentResponse struct {
	// Merchant is the name of the merchant.
	Merchant string `json:"merchant"`
	// Description is a brief description of the merchant.
	Description string `json:"description"`
	// Categories lists categories fitting the description of the merchant.
	Categories []string `json:"categories"`
	// Logo is a base64-encoded PNG or JPEG representing the merchant logo.
	Logo string `json:"logo"`
	// Location describes the country and city. May be empty if the API returns null.
	Location string `json:"location"`
	// Address describes the exact address of purchase. May be empty if the API returns null.
	Address string `json:"address"`
}

// EnrichTransactionCollectionResponse is the result of a bulk enrichment submission.
type EnrichTransactionCollectionResponse struct {
	// ID is the work ID for the enrichment request.
	ID string `json:"id"`
	// Link is the URL to the downloadable tar.gz results archive.
	Link string `json:"link"`
}

// EnrichmentCollectionStatus represents the processing state of a bulk enrichment job.
type EnrichmentCollectionStatus string

const (
	EnrichmentCollectionStatusReady   EnrichmentCollectionStatus = "READY"
	EnrichmentCollectionStatusFailed  EnrichmentCollectionStatus = "FAILED"
	EnrichmentCollectionStatusPending EnrichmentCollectionStatus = "PENDING"
)

// Enrichment defines the contract for all XYO transaction enrichment operations.
type Enrichment interface {
	// EnrichTransaction enriches a single payment transaction synchronously.
	EnrichTransaction(ctx context.Context, req *EnrichmentRequest) (*EnrichmentResponse, error)

	// EnrichTransactionWithOptions enriches a single payment transaction synchronously with optional request options.
	EnrichTransactionWithOptions(ctx context.Context, req *EnrichmentRequest, opts *RequestOptions) (*EnrichmentResponse, error)

	// EnrichTransactions submits a bulk enrichment request asynchronously
	// and returns a job ID and download link.
	EnrichTransactions(ctx context.Context, reqs []*EnrichmentRequest) (*EnrichTransactionCollectionResponse, error)

	// EnrichTransactionsWithOptions submits a bulk enrichment request asynchronously with optional multi-tenant or tracing options.
	EnrichTransactionsWithOptions(ctx context.Context, reqs []*EnrichmentRequest, opts *RequestOptions) (*EnrichTransactionCollectionResponse, error)

	// GetEnrichmentStatus returns the processing status of a bulk enrichment job.
	GetEnrichmentStatus(ctx context.Context, id string) (EnrichmentCollectionStatus, error)

	// GetEnrichmentStatusWithOptions returns the processing status with optional multi-tenant or tracing options.
	GetEnrichmentStatusWithOptions(ctx context.Context, id string, opts *RequestOptions) (EnrichmentCollectionStatus, error)

	// --- Backward-compatible aliases (retained for existing integrations) ---

	// EnrichTransactionCollection is an alias for EnrichTransactions.
	EnrichTransactionCollection(ctx context.Context, reqs []*EnrichmentRequest) (*EnrichTransactionCollectionResponse, error)

	// EnrichTransactionCollectionStatus is an alias for GetEnrichmentStatus.
	EnrichTransactionCollectionStatus(ctx context.Context, id string) (EnrichmentCollectionStatus, error)

	// DownloadEnrichmentCollection downloads and decodes a bulk enrichment result tarball.
	DownloadEnrichmentCollection(ctx context.Context, downloadURL string) ([]*EnrichmentResponse, error)
}

// Validate checks that the enrichment request fields meet business and ISO constraints.
func (r *EnrichmentRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("request is nil")
	}
	content := strings.TrimSpace(r.Content)
	if content == "" {
		return fmt.Errorf("Content cannot be empty")
	}
	if utf8.RuneCountInString(content) > 128 {
		return fmt.Errorf("Content exceeds maximum allowed length of 128 characters")
	}
	countryCode := strings.TrimSpace(r.CountryCode)
	if countryCode == "" {
		return fmt.Errorf("CountryCode cannot be empty")
	}
	if utf8.RuneCountInString(countryCode) != 2 {
		return fmt.Errorf("CountryCode must be exactly 2 characters (ISO 3166-1 alpha-2)")
	}
	return nil
}

// EnrichTransaction enriches a single payment transaction.
func (c *client) EnrichTransaction(ctx context.Context, req *EnrichmentRequest) (*EnrichmentResponse, error) {
	return c.EnrichTransactionWithOptions(ctx, req, nil)
}

// EnrichTransactionWithOptions enriches a single payment transaction synchronously with optional request options.
func (c *client) EnrichTransactionWithOptions(ctx context.Context, req *EnrichmentRequest, opts *RequestOptions) (*EnrichmentResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("xyo: EnrichTransaction: %w", err)
	}

	cid, err := extractCorrelationID(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("xyo: EnrichTransaction: %w", err)
	}

	tp, err := extractTraceparent(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("xyo: EnrichTransaction: %w", err)
	}

	genReq := openapi.NewEnrichmentRequest(req.Content, req.CountryCode)
	apiReq := c.apiClient.EnrichmentAPI.EnrichTransaction(ctx).EnrichmentRequest(*genReq)

	if cid != "" {
		apiReq = apiReq.XCorrelationID(cid)
	}
	if tp != "" {
		apiReq = apiReq.Traceparent(tp)
	}

	resp, httpResp, err := apiReq.Execute()
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return nil, parseOpenAPIError(err, "EnrichTransaction", httpResp)
	}

	return &EnrichmentResponse{
		Merchant:    resp.GetMerchant(),
		Description: resp.GetDescription(),
		Categories:  resp.GetCategories(),
		Logo:        resp.GetLogo(),
		Location:    resp.GetLocation(),
		Address:     resp.GetAddress(),
	}, nil
}

// EnrichTransactions submits a bulk enrichment request asynchronously.
func (c *client) EnrichTransactions(ctx context.Context, reqs []*EnrichmentRequest) (*EnrichTransactionCollectionResponse, error) {
	return c.EnrichTransactionsWithOptions(ctx, reqs, nil)
}

// EnrichTransactionsWithOptions submits a bulk enrichment request asynchronously with optional multi-tenant or tracing options.
func (c *client) EnrichTransactionsWithOptions(ctx context.Context, reqs []*EnrichmentRequest, opts *RequestOptions) (*EnrichTransactionCollectionResponse, error) {
	if len(reqs) == 0 {
		return nil, fmt.Errorf("xyo: EnrichTransactions: reqs slice cannot be empty (must contain between 1 and 50,000 items)")
	}
	if len(reqs) > 50000 {
		return nil, fmt.Errorf("xyo: EnrichTransactions: reqs slice exceeds maximum allowed length of 50,000 items (got %d)", len(reqs))
	}

	if opts != nil && opts.APIUser != "" {
		if strings.ContainsAny(opts.APIUser, "\r\n") {
			return nil, fmt.Errorf("xyo: EnrichTransactions: apiUser contains invalid CRLF characters")
		}
	}

	cid, err := extractCorrelationID(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("xyo: EnrichTransactions: %w", err)
	}

	tp, err := extractTraceparent(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("xyo: EnrichTransactions: %w", err)
	}

	items := make([]openapi.EnrichTransactionsRequestInner, 0, len(reqs))
	for i, req := range reqs {
		if err := req.Validate(); err != nil {
			return nil, fmt.Errorf("xyo: EnrichTransactions: request at index %d invalid: %w", i, err)
		}
		items = append(items, openapi.EnrichTransactionsRequestInner{
			Content:     req.Content,
			CountryCode: req.CountryCode,
		})
	}

	apiReq := c.apiClient.EnrichmentAPI.EnrichTransactions(ctx).
		EnrichTransactionsRequestInner(items)

	if opts != nil && opts.APIUser != "" {
		apiReq = apiReq.XApiUser(opts.APIUser)
	}
	if cid != "" {
		apiReq = apiReq.XCorrelationID(cid)
	}
	if tp != "" {
		apiReq = apiReq.Traceparent(tp)
	}

	resp, httpResp, err := apiReq.Execute()
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return nil, parseOpenAPIError(err, "EnrichTransactions", httpResp)
	}

	return &EnrichTransactionCollectionResponse{
		ID:   resp.GetId(),
		Link: resp.GetLink(),
	}, nil
}

const (
	// DefaultMaxTarEntries is the maximum number of entries processed from a bulk enrichment tarball.
	DefaultMaxTarEntries = 50000
	// DefaultMaxEntryBytes is the maximum allowed size (in bytes) for a single JSON file within the tarball (10 MiB).
	DefaultMaxEntryBytes = 10 * 1024 * 1024
	// DefaultMaxArchiveBytes is the maximum total uncompressed bytes allowed across the tarball stream (100 MiB).
	DefaultMaxArchiveBytes = 100 * 1024 * 1024
)

type maxBytesReader struct {
	r     io.Reader
	limit int64
	read  int64
	desc  string
}

func (m *maxBytesReader) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	m.read += int64(n)
	if m.read > m.limit {
		return n, fmt.Errorf("xyo: %s exceeded maximum limit of %d bytes", m.desc, m.limit)
	}
	return n, err
}

func sanitizeTarEntryName(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '\n' || r == '\r' {
			return '_'
		}
		return r
	}, name)
}

// GetEnrichmentStatus polls the status of an asynchronous bulk enrichment job by work ID.
func (c *client) GetEnrichmentStatus(ctx context.Context, id string) (EnrichmentCollectionStatus, error) {
	return c.GetEnrichmentStatusWithOptions(ctx, id, nil)
}

// GetEnrichmentStatusWithOptions polls the status of an asynchronous bulk enrichment job with optional multi-tenant or tracing options.
func (c *client) GetEnrichmentStatusWithOptions(ctx context.Context, id string, opts *RequestOptions) (EnrichmentCollectionStatus, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("xyo: GetEnrichmentStatus: id cannot be empty")
	}

	if opts != nil && opts.APIUser != "" {
		if strings.ContainsAny(opts.APIUser, "\r\n") {
			return "", fmt.Errorf("xyo: GetEnrichmentStatus: apiUser contains invalid CRLF characters")
		}
	}

	cid, err := extractCorrelationID(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("xyo: GetEnrichmentStatus: %w", err)
	}

	tp, err := extractTraceparent(ctx, opts)
	if err != nil {
		return "", fmt.Errorf("xyo: GetEnrichmentStatus: %w", err)
	}

	apiReq := c.apiClient.EnrichmentAPI.GetEnrichmentStatus(ctx, id)
	if opts != nil && opts.APIUser != "" {
		apiReq = apiReq.XApiUser(opts.APIUser)
	}
	if cid != "" {
		apiReq = apiReq.XCorrelationID(cid)
	}
	if tp != "" {
		apiReq = apiReq.Traceparent(tp)
	}

	resp, httpResp, err := apiReq.Execute()
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return "", parseOpenAPIError(err, "GetEnrichmentStatus", httpResp)
	}

	return EnrichmentCollectionStatus(resp.GetStatus()), nil
}

// --- Backward-compatible alias methods ---

// EnrichTransactionCollection is an alias for EnrichTransactions.
func (c *client) EnrichTransactionCollection(ctx context.Context, reqs []*EnrichmentRequest) (*EnrichTransactionCollectionResponse, error) {
	return c.EnrichTransactions(ctx, reqs)
}

// EnrichTransactionCollectionStatus is an alias for GetEnrichmentStatus.
func (c *client) EnrichTransactionCollectionStatus(ctx context.Context, id string) (EnrichmentCollectionStatus, error) {
	return c.GetEnrichmentStatus(ctx, id)
}

// DownloadEnrichmentCollection downloads and decodes a bulk enrichment result tarball
// from the URL returned by EnrichTransactions.
func (c *client) DownloadEnrichmentCollection(ctx context.Context, downloadURL string) ([]*EnrichmentResponse, error) {
	if downloadURL == "" {
		return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: downloadURL cannot be empty")
	}

	parsedDownloadURL, err := url.Parse(downloadURL)
	if err != nil {
		return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: parse downloadURL: %w", err)
	}
	if parsedDownloadURL.Scheme != "http" && parsedDownloadURL.Scheme != "https" {
		return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: invalid URL scheme %q (only http and https are permitted)", parsedDownloadURL.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: build request: %w", err)
	}
	req.Header.Set("Accept", "application/gzip, application/x-tar, application/octet-stream;q=0.9, */*;q=0.8")
	if userAgent := c.apiClient.GetConfig().UserAgent; userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	// Validate permitted domain for secure archive download under Zero-Trust policy
	if parsedDownloadURL.Host != "" {
		isAllowed := false
		parsedBaseURL, parseBaseErr := url.Parse(c.apiBaseURL)
		isAPIHost := parseBaseErr == nil && parsedBaseURL.Host != "" && strings.EqualFold(parsedDownloadURL.Host, parsedBaseURL.Host)
		if isAPIHost {
			isAllowed = true
		} else {
			downHost := parsedDownloadURL.Host
			downHostname := parsedDownloadURL.Hostname()
			for _, trusted := range c.trustedDownloadHosts {
				if strings.EqualFold(downHost, trusted) || strings.EqualFold(downHostname, trusted) || strings.HasSuffix(strings.ToLower(downHostname), "."+strings.ToLower(trusted)) {
					isAllowed = true
					break
				}
			}
		}

		if !isAllowed {
			return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: domain %q is not permitted for secure archive downloads", parsedDownloadURL.Host)
		}

		if isAPIHost && c.keySupplier != nil {
			if key := c.keySupplier(); key != "" {
				req.Header.Set("Authorization", "Bearer "+key)
			}
		}
	}

	httpCl := c.apiClient.GetConfig().HTTPClient
	if httpCl == nil {
		httpCl = NewDefaultHTTPClient()
	}

	resp, err := httpCl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && len(errResp.Errors) > 0 {
			errResp.HTTPStatusCode = resp.StatusCode
			return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: %w", &errResp)
		}
		return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: status %d", resp.StatusCode)
	}

	// Validate Content-Type header to diagnose intermediate proxy/WAF challenge pages
	ct := resp.Header.Get("Content-Type")
	if ct != "" {
		ctLower := strings.ToLower(ct)
		if !strings.Contains(ctLower, "gzip") && !strings.Contains(ctLower, "tar") && !strings.Contains(ctLower, "octet-stream") && !strings.Contains(ctLower, "binary") {
			return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: unexpected Content-Type %q received when expecting binary archive", ct)
		}
	}

	// Limit total compressed stream and decompressed stream to prevent decompression bombs (actively errors on overflow)
	limitedBody := &maxBytesReader{r: resp.Body, limit: DefaultMaxArchiveBytes, desc: "compressed archive stream"}
	gzReader, err := gzip.NewReader(limitedBody)
	if err != nil {
		return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: gzip stream: %w", err)
	}
	defer func() { _ = gzReader.Close() }()

	limitedTarStream := &maxBytesReader{r: gzReader, limit: DefaultMaxArchiveBytes, desc: "decompressed archive stream"}
	tarReader := tar.NewReader(limitedTarStream)
	var results []*EnrichmentResponse

	for entryCount := 0; ; entryCount++ {
		if entryCount >= DefaultMaxTarEntries {
			return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: tarball contains too many entries (exceeds limit of %d)", DefaultMaxTarEntries)
		}

		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: tar next: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		// Zip-Slip and path traversal mitigation
		if strings.Contains(header.Name, "..") || strings.HasPrefix(header.Name, "/") || strings.HasPrefix(header.Name, "\\") {
			continue
		}
		if header.Size > DefaultMaxEntryBytes {
			return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: entry %q exceeds maximum allowed size (%d bytes > %d bytes)", header.Name, header.Size, DefaultMaxEntryBytes)
		}

		var result EnrichmentResponse
		entryReader := io.LimitReader(tarReader, DefaultMaxEntryBytes)
		if err := json.NewDecoder(entryReader).Decode(&result); err != nil {
			return nil, fmt.Errorf("xyo: DownloadEnrichmentCollection: decode json from %s: %w", sanitizeTarEntryName(header.Name), err)
		}
		results = append(results, &result)
	}

	return results, nil
}
