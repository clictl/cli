// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package executor

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/clictl/cli/internal/config"
	"github.com/clictl/cli/internal/httpcache"
	"github.com/clictl/cli/internal/logger"
	"github.com/clictl/cli/internal/models"
	"github.com/clictl/cli/internal/transform"
)

// Version is the CLI version, set via SetVersion. Used for User-Agent header.
var version = "dev"

// SetVersion sets the executor package version, used in the User-Agent header.
func SetVersion(v string) {
	version = v
}

// HTTPExecutor handles execution of HTTP-protocol tool actions.
// If Cache is set, responses are cached per RFC 7234.
// If Config is set, disabled tool checks are enforced before execution.
// If PaginateAll is set, actions with Pagination config will fetch all pages.
// If SkipTransforms is set, response transforms are not applied (raw JSON output).
type HTTPExecutor struct {
	Cache          *httpcache.Cache
	Config         *config.Config
	PaginateAll    bool
	SkipTransforms bool
}

// httpResponse holds the result of a single HTTP request.
type httpResponse struct {
	StatusCode int
	Body       []byte
	Header     http.Header
}

// Execute runs the full HTTP pipeline: pre-request transforms -> retry loop -> assertions -> output transforms.
// The SkipTransforms flag bypasses output transforms for --json mode (programmatic consumption).
// Also supports response caching with conditional requests (ETag/Last-Modified),
// content-encoding negotiation (gzip, deflate), retry with backoff, and pagination.
func (e *HTTPExecutor) Execute(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]string) ([]byte, error) {
	if e.Config != nil && e.Config.IsToolDisabled(spec.Name) {
		return nil, fmt.Errorf("tool %q is disabled. Run 'clictl enable %s' to re-enable it", spec.Name, spec.Name)
	}

	// Apply pre-request transforms (transform steps with on: "request")
	var requestBody string
	if len(action.Transform) > 0 {
		var preRaw []any
		for _, ts := range action.Transform {
			if ts.On == "request" {
				// Convert TransformStep to raw map for ParsePreSteps
				m := make(map[string]any)
				if ts.Type != "" {
					m["type"] = ts.Type
				}
				if ts.DefaultParams != nil {
					dp := make(map[string]any, len(ts.DefaultParams))
					for k, v := range ts.DefaultParams {
						dp[k] = v
					}
					m["default_params"] = dp
				}
				if ts.RenameParams != nil {
					rp := make(map[string]any, len(ts.RenameParams))
					for k, v := range ts.RenameParams {
						rp[k] = v
					}
					m["rename_params"] = rp
				}
				if ts.TemplateBody != "" {
					m["template_body"] = ts.TemplateBody
				}
				preRaw = append(preRaw, m)
			}
		}
		if len(preRaw) > 0 {
			preSteps, err := transform.ParsePreSteps(preRaw)
			if err != nil {
				return nil, fmt.Errorf("parsing pre-request transforms: %w", err)
			}
			if len(preSteps) > 0 {
				reqData := &transform.RequestData{
					Params: params,
				}
				if err := preSteps.Apply(reqData); err != nil {
					return nil, fmt.Errorf("applying pre-request transforms: %w", err)
				}
				params = reqData.Params
				requestBody = reqData.Body
			}
		}
	}

	// If streaming is enabled, use the streaming path
	if action.Stream {
		return e.executeStream(ctx, spec, action, params, requestBody)
	}

	// If pagination is configured and --all is set, paginate
	if e.PaginateAll && action.Pagination != nil {
		return e.executePaginated(ctx, spec, action, params, requestBody)
	}

	// Single request with retry
	hr, err := e.doRequestWithRetry(ctx, spec, action, params, requestBody)
	if err != nil {
		return nil, err
	}

	// Run assertions before transforms if configured
	if err := runAssertions(action, hr); err != nil {
		return nil, err
	}

	if e.SkipTransforms {
		return hr.Body, nil
	}

	return applyActionTransform(hr.Body, action, spec.ResolveActionURL(action))
}

// doRequestWithRetry executes a single HTTP request with retry logic.
// If action.Retry is nil, defaults are used: on=[429,500,502,503], max_attempts=3,
// backoff=exponential, delay=1s.
func (e *HTTPExecutor) doRequestWithRetry(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]string, requestBody string) (*httpResponse, error) {
	retry := resolveRetryConfig(action.Retry)

	var lastResp *httpResponse
	var lastErr error

	for attempt := 1; attempt <= retry.maxAttempts; attempt++ {
		hr, err := e.doSingleRequest(ctx, spec, action, params, requestBody)
		if err != nil {
			// Network error: retry if we have attempts left
			lastErr = err
			if attempt < retry.maxAttempts {
				delay := retry.delayForAttempt(attempt)
				fmt.Fprintf(os.Stderr, "[retry] attempt %d/%d after network error (waiting %s)\n", attempt+1, retry.maxAttempts, delay)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
				}
				continue
			}
			return nil, lastErr
		}

		lastResp = hr

		// Check if the status code is retryable
		if !retry.shouldRetry(hr.StatusCode) {
			break
		}

		if attempt >= retry.maxAttempts {
			break
		}

		// Determine delay: Retry-After header takes priority
		delay := retryAfterDelay(hr.Header)
		if delay == 0 {
			delay = retry.delayForAttempt(attempt)
		}

		fmt.Fprintf(os.Stderr, "[retry] attempt %d/%d after %d (waiting %s)\n", attempt+1, retry.maxAttempts, hr.StatusCode, delay)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	if lastResp == nil {
		return nil, lastErr
	}

	// Check for non-2xx after retries exhausted
	if lastResp.StatusCode < 200 || lastResp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", lastResp.StatusCode, string(lastResp.Body))
	}

	return lastResp, nil
}

// doSingleRequest performs a single HTTP request without retry.
func (e *HTTPExecutor) doSingleRequest(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]string, requestBody string) (*httpResponse, error) {
	baseURL := spec.ResolveActionURL(action)
	fullURL, err := buildURL(baseURL, action, params)
	if err != nil {
		return nil, fmt.Errorf("building request URL: %w", err)
	}

	method := http.MethodGet
	if action.Method != "" {
		method = strings.ToUpper(action.Method)
	}

	if logger.IsEnabled() {
		fmt.Fprintf(os.Stderr, "[debug] %s %s\n", method, fullURL)
	}

	// Check cache for a fresh response
	if e.Cache != nil && method == http.MethodGet {
		entry, _ := e.Cache.Get(method, fullURL)
		if entry != nil && !entry.IsExpired() {
			body, err := applyActionTransform(entry.Body, action)
			if err != nil {
				return nil, err
			}
			return &httpResponse{StatusCode: 200, Body: body, Header: http.Header{}}, nil
		}
	}

	var reqBodyReader io.Reader
	if requestBody != "" {
		reqBodyReader = bytes.NewBufferString(requestBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}

	for k, v := range spec.ResolveActionHeaders(action) {
		req.Header.Set(k, v)
	}

	// Only advertise encodings we can decompress
	req.Header.Set("Accept-Encoding", "gzip, deflate, identity")

	// Set User-Agent (spec headers can override)
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "clictl/"+version+" (https://clictl.dev)")
	}

	logger.Debug("HTTP request", logger.F("method", method), logger.F("url", fullURL))

	// Safety check for mutating actions
	if err := confirmMutableAction(method, action, confirmedActions); err != nil {
		return nil, err
	}

	if err := applyAuth(req, spec.ResolveActionAuth(action)); err != nil {
		return nil, fmt.Errorf("applying auth: %w", err)
	}

	// Add conditional request headers from stale cache entry
	if e.Cache != nil && method == http.MethodGet {
		entry, _ := e.Cache.Get(method, fullURL)
		if entry != nil && entry.HasValidator() {
			httpcache.ConditionalHeaders(req, entry)
		}
	}

	// Build HTTP transport
	transport := &http.Transport{
		DisableCompression: true, // We handle decompression for caching
	}

	timeout := 30 * time.Second
	if spec.Server != nil {
		timeout = spec.Server.TimeoutDuration()
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	resp, err := client.Do(req)
	if err != nil {
		// On network error, try serving from stale cache
		if e.Cache != nil {
			entry, _ := e.Cache.Get(method, fullURL)
			if entry != nil {
				body, tErr := applyActionTransform(entry.Body, action)
				if tErr == nil {
					return &httpResponse{StatusCode: 200, Body: body, Header: http.Header{}}, nil
				}
			}
		}
		return nil, fmt.Errorf("executing HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// 304 Not Modified - serve from cache
	if resp.StatusCode == http.StatusNotModified && e.Cache != nil {
		entry, _ := e.Cache.Get(method, fullURL)
		if entry != nil {
			body, tErr := applyActionTransform(entry.Body, action)
			if tErr == nil {
				return &httpResponse{StatusCode: 200, Body: body, Header: resp.Header}, nil
			}
		}
	}

	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	logger.Debug("HTTP response", logger.F("status", resp.StatusCode), logger.F("size", len(body)))

	// Store in cache if eligible
	if e.Cache != nil && httpcache.IsCacheable(method, resp.StatusCode, resp.Header) {
		entry := httpcache.EntryFromResponse(method, fullURL, resp, body)
		_ = e.Cache.Put(entry)
	}

	return &httpResponse{StatusCode: resp.StatusCode, Body: body, Header: resp.Header}, nil
}

// ---------------------------------------------------------------------------
// Retry helpers
// ---------------------------------------------------------------------------

// resolvedRetry holds computed retry configuration.
type resolvedRetry struct {
	on          []int
	maxAttempts int
	backoff     string
	delay       time.Duration
}

// resolveRetryConfig normalizes retry config with defaults.
func resolveRetryConfig(r *models.Retry) resolvedRetry {
	rr := resolvedRetry{
		on:          []int{429, 500, 502, 503},
		maxAttempts: 3,
		backoff:     "exponential",
		delay:       1 * time.Second,
	}
	if r == nil {
		return rr
	}
	if len(r.On) > 0 {
		rr.on = r.On
	}
	if r.MaxAttempts > 0 {
		rr.maxAttempts = r.MaxAttempts
	}
	if r.Backoff != "" {
		rr.backoff = r.Backoff
	}
	if r.Delay != "" {
		if d, err := time.ParseDuration(r.Delay); err == nil {
			rr.delay = d
		}
	}
	return rr
}

// shouldRetry returns true if the given status code should trigger a retry.
func (rr *resolvedRetry) shouldRetry(statusCode int) bool {
	for _, code := range rr.on {
		if code == statusCode {
			return true
		}
	}
	return false
}

// delayForAttempt computes the delay before the next attempt (1-indexed attempt number).
// For attempt=1, this is the delay before attempt 2.
func (rr *resolvedRetry) delayForAttempt(attempt int) time.Duration {
	switch rr.backoff {
	case "exponential":
		// delay doubles each attempt: delay * 2^(attempt-1)
		multiplier := time.Duration(1)
		for i := 1; i < attempt; i++ {
			multiplier *= 2
		}
		return rr.delay * multiplier
	case "linear", "fixed":
		return rr.delay
	default:
		return rr.delay
	}
}

// retryAfterDelay parses the Retry-After header. Returns 0 if not present or unparseable.
func retryAfterDelay(header http.Header) time.Duration {
	val := header.Get("Retry-After")
	if val == "" {
		return 0
	}
	// Try as seconds first
	if seconds, err := strconv.Atoi(val); err == nil {
		return time.Duration(seconds) * time.Second
	}
	// Try as HTTP date
	if t, err := http.ParseTime(val); err == nil {
		delay := time.Until(t)
		if delay > 0 {
			return delay
		}
	}
	return 0
}

// runAssertions executes response assertions if configured on the action.
func runAssertions(action *models.Action, hr *httpResponse) error {
	if len(action.Assert) == 0 {
		return nil
	}
	assertRaw := make([]any, len(action.Assert))
	for i, a := range action.Assert {
		m := map[string]any{"type": a.Type}
		if len(a.Values) > 0 {
			m["values"] = a.Values
		}
		if a.Exists != "" {
			m["exists"] = a.Exists
		}
		if a.NotEmpty != "" {
			m["not_empty"] = a.NotEmpty
		}
		if a.Filter != "" {
			m["filter"] = a.Filter
		}
		if a.Script != "" {
			m["script"] = a.Script
		}
		if a.Expression != "" {
			m["expression"] = a.Expression
		}
		if a.Value != "" {
			m["value"] = a.Value
		}
		assertRaw[i] = m
	}
	assertions, err := transform.ParseAssertions(assertRaw)
	if err != nil {
		return fmt.Errorf("parsing assert config: %w", err)
	}
	if err := assertions.Check(hr.StatusCode, hr.Body); err != nil {
		return fmt.Errorf("response assertion failed: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Pagination
// ---------------------------------------------------------------------------

// executePaginated fetches all pages and concatenates the results into a single array.
// Transforms are applied once on the combined result, not per page.
func (e *HTTPExecutor) executePaginated(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]string, requestBody string) ([]byte, error) {
	pg := action.Pagination
	maxPages := pg.MaxPages
	if maxPages <= 0 {
		maxPages = 100 // safety limit
	}

	var allResults []json.RawMessage
	pageParams := copyParams(params)

	// Set per_page/limit defaults
	if pg.PerPageParam != "" && pg.PerPageDefault > 0 {
		if _, ok := pageParams[pg.PerPageParam]; !ok {
			pageParams[pg.PerPageParam] = strconv.Itoa(pg.PerPageDefault)
		}
	}
	if pg.LimitParam != "" && pg.LimitDefault > 0 {
		if _, ok := pageParams[pg.LimitParam]; !ok {
			pageParams[pg.LimitParam] = strconv.Itoa(pg.LimitDefault)
		}
	}

	// Initialize pagination state
	switch pg.Type {
	case "page":
		if _, ok := pageParams[pg.Param]; !ok {
			pageParams[pg.Param] = "1"
		}
	case "offset":
		if _, ok := pageParams[pg.Param]; !ok {
			pageParams[pg.Param] = "0"
		}
	}

	for page := 0; page < maxPages; page++ {
		hr, err := e.doRequestWithRetry(ctx, spec, action, pageParams, requestBody)
		if err != nil {
			if len(allResults) > 0 {
				// Return what we have so far
				break
			}
			return nil, err
		}

		// Parse the response body
		var pageData json.RawMessage
		if err := json.Unmarshal(hr.Body, &pageData); err != nil {
			if len(allResults) > 0 {
				break
			}
			return nil, fmt.Errorf("pagination: cannot parse response as JSON: %w", err)
		}

		// Try to extract an array from the response
		items := extractArray(hr.Body)
		if items == nil {
			// If the response is already an array, use it directly
			var arr []json.RawMessage
			if json.Unmarshal(hr.Body, &arr) == nil {
				items = arr
			}
		}

		if items == nil || len(items) == 0 {
			break
		}

		allResults = append(allResults, items...)
		logger.Debug("pagination", logger.F("page", page+1), logger.F("items", len(items)), logger.F("total", len(allResults)))

		// Check stop conditions
		if pg.HasMorePath != "" {
			hasMore := extractJSONPathBool(hr.Body, pg.HasMorePath)
			if !hasMore {
				break
			}
		}

		// Advance pagination state
		switch pg.Type {
		case "page":
			current, _ := strconv.Atoi(pageParams[pg.Param])
			pageParams[pg.Param] = strconv.Itoa(current + 1)

		case "cursor":
			cursor := extractJSONPathString(hr.Body, pg.CursorPath)
			if cursor == "" {
				// No more pages
				goto done
			}
			pageParams[pg.Param] = cursor

		case "offset":
			current, _ := strconv.Atoi(pageParams[pg.Param])
			limit := pg.LimitDefault
			if limit <= 0 {
				limit = pg.PerPageDefault
			}
			if limit <= 0 {
				limit = len(items)
			}
			pageParams[pg.Param] = strconv.Itoa(current + limit)
		}
	}

done:
	// Combine all results into a JSON array
	combined, err := json.Marshal(allResults)
	if err != nil {
		return nil, fmt.Errorf("pagination: marshaling combined results: %w", err)
	}

	// Apply transforms once on the combined result
	return applyActionTransform(combined, action)
}

// copyParams creates a shallow copy of a params map.
func copyParams(params map[string]string) map[string]string {
	out := make(map[string]string, len(params))
	for k, v := range params {
		out[k] = v
	}
	return out
}

// extractArray attempts to find an array in a JSON object response.
// It looks for common array field names in order of likelihood.
func extractArray(body []byte) []json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil
	}

	// Try common field names
	for _, key := range []string{"data", "items", "results", "records", "entries", "list", "nodes", "edges", "values"} {
		if raw, ok := obj[key]; ok {
			var arr []json.RawMessage
			if json.Unmarshal(raw, &arr) == nil {
				return arr
			}
		}
	}

	// If none of the common names match, try the first field that is an array
	for _, raw := range obj {
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
			return arr
		}
	}

	return nil
}

// extractJSONPathBool extracts a boolean value from a simple dot-notation path.
func extractJSONPathBool(body []byte, path string) bool {
	val := extractJSONPathValue(body, path)
	if val == nil {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != "" && v != "false" && v != "0"
	default:
		return false
	}
}

// extractJSONPathString extracts a string value from a simple dot-notation path.
func extractJSONPathString(body []byte, path string) string {
	val := extractJSONPathValue(body, path)
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// extractJSONPathValue traverses a JSON object using a simple dot-notation path.
func extractJSONPathValue(body []byte, path string) any {
	path = strings.TrimPrefix(path, "$.")
	if path == "" || path == "$" {
		return nil
	}

	parts := strings.Split(path, ".")
	var current any
	if err := json.Unmarshal(body, &current); err != nil {
		return nil
	}

	for _, part := range parts {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		val, exists := obj[part]
		if !exists {
			return nil
		}
		current = val
	}
	return current
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

// executeStream reads the HTTP response body line-by-line and writes each line
// to stdout immediately. If no new data arrives within StreamTimeout, it stops.
func (e *HTTPExecutor) executeStream(ctx context.Context, spec *models.ToolSpec, action *models.Action, params map[string]string, requestBody string) ([]byte, error) {
	baseURL := spec.ResolveActionURL(action)
	fullURL, err := buildURL(baseURL, action, params)
	if err != nil {
		return nil, fmt.Errorf("building request URL: %w", err)
	}

	method := http.MethodGet
	if action.Method != "" {
		method = strings.ToUpper(action.Method)
	}

	var reqBodyReader io.Reader
	if requestBody != "" {
		reqBodyReader = bytes.NewBufferString(requestBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}

	for k, v := range spec.ResolveActionHeaders(action) {
		req.Header.Set(k, v)
	}

	req.Header.Set("Accept-Encoding", "identity") // No compression for streaming
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "clictl/"+version+" (https://clictl.dev)")
	}

	if err := applyAuth(req, spec.ResolveActionAuth(action)); err != nil {
		return nil, fmt.Errorf("applying auth: %w", err)
	}

	// Parse stream timeout
	streamTimeout := 30 * time.Second
	if action.StreamTimeout != "" {
		if action.StreamTimeout == "0" {
			streamTimeout = 0 // disabled
		} else if d, pErr := time.ParseDuration(action.StreamTimeout); pErr == nil {
			streamTimeout = d
		}
	}

	timeout := 0 * time.Second // No overall timeout for streaming
	transport := &http.Transport{
		DisableCompression: true,
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing streaming HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := readBody(resp)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)

	var allData bytes.Buffer

	for {
		// Set up idle timeout
		readDone := make(chan bool, 1)
		go func() {
			readDone <- scanner.Scan()
		}()

		if streamTimeout > 0 {
			select {
			case <-ctx.Done():
				return allData.Bytes(), ctx.Err()
			case ok := <-readDone:
				if !ok {
					return allData.Bytes(), scanner.Err()
				}
			case <-time.After(streamTimeout):
				logger.Debug("stream idle timeout reached", logger.F("timeout", streamTimeout.String()))
				return allData.Bytes(), nil
			}
		} else {
			// No timeout, wait indefinitely
			select {
			case <-ctx.Done():
				return allData.Bytes(), ctx.Err()
			case ok := <-readDone:
				if !ok {
					return allData.Bytes(), scanner.Err()
				}
			}
		}

		line := scanner.Text()
		fmt.Fprintln(os.Stdout, line)
		allData.WriteString(line)
		allData.WriteByte('\n')
	}
}

// readBody reads and decompresses the response body based on Content-Encoding.
func readBody(resp *http.Response) ([]byte, error) {
	encoding := strings.ToLower(resp.Header.Get("Content-Encoding"))

	switch encoding {
	case "gzip":
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			// Gzip header invalid - try reading raw
			return io.ReadAll(resp.Body)
		}
		defer gz.Close()
		return io.ReadAll(gz)

	case "deflate":
		// Try zlib first (RFC 1950, most common for HTTP "deflate")
		zr, err := zlib.NewReader(resp.Body)
		if err == nil {
			defer zr.Close()
			return io.ReadAll(zr)
		}
		// Fall back to raw deflate (RFC 1951)
		dr := flate.NewReader(resp.Body)
		defer dr.Close()
		return io.ReadAll(dr)

	case "":
		// No encoding, read raw
		return io.ReadAll(resp.Body)

	default:
		// Unknown encoding (e.g. br/brotli) - read raw bytes.
		// This may produce garbled output, but it avoids a hard error.
		// Log a warning so the user knows.
		logger.Warn("unsupported Content-Encoding, output may be garbled",
			logger.F("encoding", encoding))
		return io.ReadAll(resp.Body)
	}
}

// applyActionTransform applies the action's output transforms if configured.
// Looks for transform steps with on: "output" or on: "" (default phase)
// and runs them through the full transform pipeline.
func applyActionTransform(body []byte, action *models.Action, baseURLs ...string) ([]byte, error) {
	baseURL := ""
	if len(baseURLs) > 0 {
		baseURL = baseURLs[0]
	}
	if len(action.Transform) == 0 {
		return body, nil
	}

	// Collect output-phase transform steps
	var outputSteps []any
	for _, ts := range action.Transform {
		if ts.On != "" && ts.On != "output" {
			continue
		}
		m := modelTransformToMap(ts)
		// Inject base URL into html_to_markdown for resolving relative URLs
		if ts.Type == "html_to_markdown" && baseURL != "" {
			if cfg, ok := m["html_to_markdown"].(map[string]any); ok {
				if cfg["base_url"] == nil || cfg["base_url"] == "" {
					cfg["base_url"] = baseURL
				}
			}
		}
		outputSteps = append(outputSteps, m)
	}

	if len(outputSteps) == 0 {
		return body, nil
	}

	logger.Debug("applying output transforms", logger.F("step_count", len(outputSteps)))

	pipeline, err := transform.ParseSteps(outputSteps)
	if err != nil {
		return nil, fmt.Errorf("parsing output transforms: %w", err)
	}

	// Parse body into a Go value so the pipeline can operate on it.
	// Try JSON first; if the body is not valid JSON, treat it as a string.
	var input any
	if err := json.Unmarshal(body, &input); err != nil {
		input = string(body)
	}

	result, err := pipeline.Apply(input)
	if err != nil {
		return nil, fmt.Errorf("applying output transforms: %w", err)
	}

	logger.Debug("transforms applied successfully", logger.F("result_type", fmt.Sprintf("%T", result)))

	// Convert result back to bytes
	switch v := result.(type) {
	case string:
		return []byte(v), nil
	case []byte:
		return v, nil
	default:
		return json.MarshalIndent(v, "", "  ")
	}
}

// modelTransformToMap converts a models.TransformStep to a map[string]any
// suitable for transform.ParseSteps.
func modelTransformToMap(ts models.TransformStep) map[string]any {
	m := make(map[string]any)
	if ts.Type != "" {
		m["type"] = ts.Type
	}
	if ts.Extract != "" {
		m["extract"] = ts.Extract
	}
	if len(ts.Select) > 0 {
		m["select"] = ts.Select
	}
	if ts.Template != "" {
		m["template"] = ts.Template
	}
	if ts.MaxItems > 0 || ts.MaxLength > 0 {
		tc := map[string]any{}
		if ts.MaxItems > 0 {
			tc["max_items"] = ts.MaxItems
		}
		if ts.MaxLength > 0 {
			tc["max_length"] = ts.MaxLength
		}
		m["truncate"] = tc
	}
	if len(ts.Rename) > 0 {
		m["rename"] = ts.Rename
	}
	if ts.RemoveImages || ts.RemoveLinks {
		cfg := map[string]any{}
		if ts.RemoveImages {
			cfg["remove_images"] = true
		}
		if ts.RemoveLinks {
			cfg["remove_links"] = true
		}
		m["html_to_markdown"] = cfg
	} else if ts.Type == "html_to_markdown" {
		// Ensure html_to_markdown config exists even without options
		m["html_to_markdown"] = map[string]any{}
	}
	if ts.Script != "" {
		m["js"] = ts.Script
	}
	if ts.Value != "" {
		m["value"] = ts.Value
	}
	if ts.Field != "" {
		m["field"] = ts.Field
	}
	if ts.Order != "" {
		m["order"] = ts.Order
	}
	if ts.Filter != "" {
		m["filter"] = ts.Filter
	}
	if ts.Separator != "" {
		m["separator"] = ts.Separator
	}
	if ts.From != "" {
		m["from"] = ts.From
	}
	if ts.To != "" {
		m["to"] = ts.To
	}
	if ts.CSVHeaders {
		m["headers"] = true
	}
	if ts.Flatten {
		m["flatten"] = true
	}
	if ts.Unwrap {
		m["unwrap"] = true
	}
	if len(ts.Only) > 0 {
		m["only"] = ts.Only
	}
	if len(ts.Inject) > 0 {
		m["inject"] = ts.Inject
	}
	if len(ts.Patterns) > 0 {
		var patterns []any
		for _, p := range ts.Patterns {
			pm := map[string]any{"match": p.Field, "replace": p.Replace}
			patterns = append(patterns, pm)
		}
		m["redact"] = patterns
	}
	if ts.PipeTool != "" {
		m["pipe_tool"] = ts.PipeTool
	}
	if ts.PipeAction != "" {
		m["pipe_action"] = ts.PipeAction
	}
	if len(ts.PipeParams) > 0 {
		m["pipe_params"] = ts.PipeParams
	}
	if ts.PipeRun != "" {
		m["pipe_run"] = ts.PipeRun
	}
	return m
}

// allowPrivateBaseURL can be set to true in tests to skip SSRF validation
// for httptest.NewServer (which binds to 127.0.0.1).
var allowPrivateBaseURL = false

// validateBaseURL checks that a base URL does not point to a private or loopback
// address, preventing SSRF attacks via tool spec configuration.
func validateBaseURL(baseURL string) error {
	if allowPrivateBaseURL {
		return nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}
	host := parsed.Hostname()
	if host == "" {
		return nil
	}
	if isPrivateHost(host) {
		return fmt.Errorf("SSRF protection: private/loopback addresses not allowed in base_url: %s", host)
	}
	return nil
}

// isPrivateHost returns true if the given host resolves to a private, loopback,
// or link-local IP address.
func isPrivateHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

// buildURL constructs the full request URL from a base URL, action path, and parameters.
func buildURL(baseURL string, action *models.Action, params map[string]string) (string, error) {
	if err := validateBaseURL(baseURL); err != nil {
		return "", err
	}
	base := strings.TrimRight(baseURL, "/")
	path := action.Path

	for _, p := range action.Params {
		if p.In == "path" {
			val, ok := params[p.Name]
			if !ok && p.Required {
				return "", fmt.Errorf("required path parameter %q is missing", p.Name)
			}
			if ok {
				path = strings.ReplaceAll(path, "{{"+p.Name+"}}", val)
				path = strings.ReplaceAll(path, "{"+p.Name+"}", val)
			}
		}
	}

	fullURL := base + path

	queryParts := []string{}
	for _, p := range action.Params {
		if p.In != "query" {
			continue
		}
		val, ok := params[p.Name]
		if !ok {
			if p.Default != "" {
				val = p.Default
			} else if p.Required {
				return "", fmt.Errorf("required query parameter %q is missing", p.Name)
			} else {
				continue
			}
		}
		queryParts = append(queryParts, fmt.Sprintf("%s=%s", url.QueryEscape(p.Name), url.QueryEscape(val)))
	}

	if len(queryParts) > 0 {
		separator := "?"
		if strings.Contains(fullURL, "?") {
			separator = "&"
		}
		fullURL += separator + strings.Join(queryParts, "&")
	}

	return fullURL, nil
}

// confirmedActions tracks which actions have been confirmed this session.
var confirmedActions = map[string]bool{}

// confirmMutableAction prompts the user for confirmation before executing
// potentially unsafe HTTP actions. Returns nil if the action is safe or confirmed.
func confirmMutableAction(method string, action *models.Action, confirmed map[string]bool) error {
	method = strings.ToUpper(method)

	// GET, HEAD, OPTIONS are always safe
	if method == "GET" || method == "HEAD" || method == "OPTIONS" {
		return nil
	}

	// Actions not marked mutable skip confirmation
	if !action.Mutable {
		return nil
	}

	// Check if already confirmed this session
	key := action.Name + ":" + method
	if confirmed[key] {
		// DELETE always re-prompts
		if method != "DELETE" {
			return nil
		}
	}

	// Prompt user
	verb := "write to"
	if method == "DELETE" {
		verb = "DELETE from"
	}
	fmt.Fprintf(os.Stderr, "Warning: Action %q will %s the API (%s %s).\n", action.Name, verb, method, action.Path)
	fmt.Fprintf(os.Stderr, "Continue? [y/N] ")

	var response string
	fmt.Scanln(&response)
	response = strings.ToLower(strings.TrimSpace(response))
	if response != "y" && response != "yes" {
		return fmt.Errorf("action cancelled by user")
	}

	confirmed[key] = true
	return nil
}

// applyAuth injects authentication credentials into the HTTP request.
// In 1.0 format, auth.header is a template like "Authorization: Bearer ${KEY}".
func applyAuth(req *http.Request, auth *models.Auth) error {
	if auth == nil {
		return nil
	}

	// Resolve env var values for ${KEY} substitution
	resolveValue := func(tmpl string) string {
		result := tmpl
		for _, envName := range auth.Env {
			val := os.Getenv(envName)
			result = strings.ReplaceAll(result, "${"+envName+"}", val)
		}
		return result
	}

	// Check that at least one env var is set
	if len(auth.Env) > 0 {
		anySet := false
		for _, envName := range auth.Env {
			if os.Getenv(envName) != "" {
				anySet = true
				break
			}
		}
		if !anySet {
			envName := auth.Env[0]
			msg := fmt.Sprintf("environment variable %q is not set\n  Fix: export %s=your-key-here", envName, envName)
			return fmt.Errorf("%s", msg)
		}
	}

	// Header template injection: "HeaderName: value ${KEY}"
	if auth.Header != "" {
		resolved := resolveValue(auth.Header)
		// Split on first ": " to get header name and value
		if idx := strings.Index(resolved, ": "); idx > 0 {
			req.Header.Set(resolved[:idx], resolved[idx+2:])
		} else {
			// No colon separator - treat entire string as header name, env var as value
			if len(auth.Env) > 0 {
				req.Header.Set(resolved, os.Getenv(auth.Env[0]))
			}
		}
		return nil
	}

	// Query param injection
	if auth.Param != "" {
		if len(auth.Env) > 0 {
			q := req.URL.Query()
			q.Set(auth.Param, os.Getenv(auth.Env[0]))
			req.URL.RawQuery = q.Encode()
		}
		return nil
	}

	return nil
}

// applyTransform performs basic JSONPath extraction on the response body.
// Supports simple dot-notation paths like "$.main" or "$.data.items".
func applyTransform(body []byte, extract string) ([]byte, error) {
	path := strings.TrimPrefix(extract, "$.")
	if path == "" || path == "$" {
		return body, nil
	}

	parts := strings.Split(path, ".")

	var current interface{}
	if err := json.Unmarshal(body, &current); err != nil {
		return nil, fmt.Errorf("parsing JSON for transform: %w", err)
	}

	for _, part := range parts {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("cannot traverse into non-object at %q", part)
		}
		val, exists := obj[part]
		if !exists {
			return nil, fmt.Errorf("field %q not found in response", part)
		}
		current = val
	}

	result, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling transform result: %w", err)
	}
	return result, nil
}
