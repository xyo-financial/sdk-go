package xyo

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xyo-financial/sdk-go/v2/openapi"
)

// APIError represents an RFC 7807-inspired error returned by the XYO API.
type APIError struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status,omitempty"`
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
}

// ErrorResponse represents the payload of an API error response.
// The XYO API wraps errors in an "errors" array.
type ErrorResponse struct {
	// HTTPStatusCode contains the HTTP response status code that triggered the error.
	HTTPStatusCode int `json:"-"`

	// Errors contains the list of API exceptions returned by the server.
	Errors []*APIError `json:"errors"`

	// RateLimitLimit contains the RateLimit-Limit header value if present.
	RateLimitLimit int `json:"-"`

	// RateLimitRemaining contains the RateLimit-Remaining header value if present.
	RateLimitRemaining int `json:"-"`

	// RateLimitReset contains the RateLimit-Reset header value if present (Unix timestamp or seconds).
	RateLimitReset int64 `json:"-"`

	// RetryAfter contains the Retry-After header value in seconds if present.
	RetryAfter int `json:"-"`
}

// Error implements the standard error interface.
func (e *ErrorResponse) Error() string {
	if len(e.Errors) == 0 {
		return fmt.Sprintf("status %d", e.HTTPStatusCode)
	}

	if len(e.Errors) == 1 {
		err := e.Errors[0]
		return fmt.Sprintf("status %d: %s: %s", e.HTTPStatusCode, err.Title, err.Detail)
	}

	msgs := make([]string, 0, len(e.Errors))
	for _, err := range e.Errors {
		msgs = append(msgs, fmt.Sprintf("%s: %s", err.Title, err.Detail))
	}
	return fmt.Sprintf("status %d: %s", e.HTTPStatusCode, strings.Join(msgs, ", "))
}

// parseOpenAPIError converts a generated-client error into an SDK-level *ErrorResponse,
// preserving HTTP status codes, rate limit headers, and structured error fields from the API response body.
func parseOpenAPIError(err error, op string, httpResp *http.Response) error {
	if err == nil {
		return nil
	}

	statusCode := 0
	if httpResp != nil {
		statusCode = httpResp.StatusCode
	}

	var openapiErr *openapi.GenericOpenAPIError
	if errors.As(err, &openapiErr) {
		var errResp *ErrorResponse

		// Try the already-decoded model first.
		if model := openapiErr.Model(); model != nil {
			var openapiResp *openapi.ErrorResponse
			switch m := model.(type) {
			case openapi.ErrorResponse:
				openapiResp = &m
			case *openapi.ErrorResponse:
				openapiResp = m
			}
			if openapiResp != nil && len(openapiResp.Errors) > 0 {
				errResp = mapErrorResponse(openapiResp, statusCode)
			}
		}

		// Fall back to raw body parsing.
		if errResp == nil && len(openapiErr.Body()) > 0 {
			var sdkRaw ErrorResponse
			if jsonErr := json.Unmarshal(openapiErr.Body(), &sdkRaw); jsonErr == nil && len(sdkRaw.Errors) > 0 {
				sdkRaw.HTTPStatusCode = statusCode
				errResp = &sdkRaw
			}
		}

		if errResp != nil {
			extractRateLimitHeaders(httpResp, errResp)
			return fmt.Errorf("xyo: %s: %w", op, errResp)
		}

		if statusCode != 0 {
			errResp = &ErrorResponse{HTTPStatusCode: statusCode}
			extractRateLimitHeaders(httpResp, errResp)
			return fmt.Errorf("xyo: %s: %w", op, errResp)
		}
	}

	if httpResp != nil && httpResp.StatusCode != 0 {
		return fmt.Errorf("xyo: %s: status %d: %w", op, httpResp.StatusCode, err)
	}

	return fmt.Errorf("xyo: %s: %w", op, err)
}

func extractRateLimitHeaders(httpResp *http.Response, errResp *ErrorResponse) {
	if httpResp == nil || errResp == nil {
		return
	}

	getHeader := func(keys ...string) string {
		for _, k := range keys {
			if v := httpResp.Header.Get(k); v != "" {
				return v
			}
		}
		return ""
	}

	if val := getHeader("Retry-After", "X-Retry-After"); val != "" {
		if sec, err := strconv.Atoi(val); err == nil {
			errResp.RetryAfter = sec
		} else if t, err := http.ParseTime(val); err == nil {
			diff := int(math.Ceil(time.Until(t).Seconds()))
			if diff < 0 {
				diff = 0
			}
			errResp.RetryAfter = diff
		}
	}

	if val := getHeader("RateLimit-Limit", "X-RateLimit-Limit", "X-Rate-Limit-Limit"); val != "" {
		if limit, err := strconv.Atoi(val); err == nil {
			errResp.RateLimitLimit = limit
		}
	}

	if val := getHeader("RateLimit-Remaining", "X-RateLimit-Remaining", "X-Rate-Limit-Remaining"); val != "" {
		if rem, err := strconv.Atoi(val); err == nil {
			errResp.RateLimitRemaining = rem
		}
	}

	if val := getHeader("RateLimit-Reset", "X-RateLimit-Reset", "X-Rate-Limit-Reset"); val != "" {
		if reset, err := strconv.ParseInt(val, 10, 64); err == nil {
			errResp.RateLimitReset = reset
		}
	}
}

func mapErrorResponse(src *openapi.ErrorResponse, statusCode int) *ErrorResponse {
	out := &ErrorResponse{HTTPStatusCode: statusCode}
	for _, e := range src.Errors {
		out.Errors = append(out.Errors, &APIError{
			Type:     e.GetType(),
			Title:    e.GetTitle(),
			Status:   int(e.GetStatus()),
			Detail:   e.GetDetail(),
			Instance: e.GetInstance(),
		})
	}
	if out.HTTPStatusCode == 0 && len(out.Errors) > 0 {
		out.HTTPStatusCode = out.Errors[0].Status
	}
	return out
}
