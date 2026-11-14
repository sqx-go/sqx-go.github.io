// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright (C) 2026 Abhishek Sengar

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultRequestTimeout     = 15 * time.Second
	defaultCacheTTL           = 7 * 24 * time.Hour
	defaultMaxBodyBytes       = 10 * 1024 * 1024
	connectTimeout            = 10 * time.Second
	keepAliveInterval         = 30 * time.Second
	tlsHandshakeTimeout       = 10 * time.Second
	idleConnectionTimeout     = 60 * time.Second
	maxIdleConnections        = 100
	maxIdleConnectionsPerHost = 10
	dnsLookupTimeout          = 5 * time.Second
	cacheBusyTimeoutMS        = 10000
	inMemoryDatabasePath      = ":memory:"
	acceptHeaderValue         = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	addressRecordKind         = "A"
	textRecordKind            = "TXT"
)

type HTTPResponse struct {
	URL            string
	FinalURL       string
	StatusCode     int
	Headers        map[string][]string
	Body           string
	ResponseTimeMS int64
	FetchedAtUnix  int64
	Error          string
}

type FetchSettings struct {
	CacheTTL       time.Duration
	RequestTimeout time.Duration
	UserAgent      string
	MaxBodyBytes   int64
	Offline        bool
	ForceRefresh   bool
}

type WebFetcher struct {
	settings   FetchSettings
	cacheStore *sql.DB
	client     *http.Client
	responses  sync.Map
	dnsRecords sync.Map
}

func defaultFetchSettings() FetchSettings {
	return FetchSettings{
		CacheTTL:       defaultCacheTTL,
		RequestTimeout: defaultRequestTimeout,
		UserAgent:      "sqx/" + buildVersion,
		MaxBodyBytes:   defaultMaxBodyBytes,
	}
}

func defaultCachePath() string {
	directory, err := os.UserCacheDir()
	if err != nil || directory == "" {
		directory = os.TempDir()
	}
	return filepath.Join(directory, "sqx", "http_cache.db")
}

func openWebFetcher(cachePath string, settings FetchSettings) (*WebFetcher, error) {
	store, err := openCacheStore(cachePath)
	if err != nil {
		return nil, err
	}
	return &WebFetcher{
		settings:   settings,
		cacheStore: store,
		client:     newHTTPClient(settings.RequestTimeout),
	}, nil
}

func openCacheStore(path string) (*sql.DB, error) {
	if path == "" {
		path = defaultCachePath()
	}
	if path != inMemoryDatabasePath {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("creating cache directory: %w", err)
		}
	}
	store, err := sql.Open("sqlite", sqliteDSN(path,
		fmt.Sprintf("busy_timeout(%d)", cacheBusyTimeoutMS),
		"journal_mode(WAL)",
		"synchronous(NORMAL)",
		"temp_store(MEMORY)",
	))
	if err != nil {
		return nil, fmt.Errorf("opening cache db: %w", err)
	}
	store.SetMaxOpenConns(1)
	if err := createCacheSchema(store); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func createCacheSchema(store *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS http_cache (
			url TEXT PRIMARY KEY,
			final_url TEXT,
			status_code INTEGER,
			headers_json TEXT,
			body TEXT,
			response_ms INTEGER,
			fetched_at INTEGER,
			error_text TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_http_cache_fetched ON http_cache(fetched_at)`,
		`CREATE TABLE IF NOT EXISTS dns_cache (
			lookup_key TEXT PRIMARY KEY,
			records_json TEXT,
			fetched_at INTEGER
		)`,
	}
	for _, statement := range statements {
		if _, err := store.Exec(statement); err != nil {
			return fmt.Errorf("initializing cache schema: %w", err)
		}
	}
	return nil
}

func newHTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   connectTimeout,
			KeepAlive: keepAliveInterval,
		}).DialContext,
		MaxIdleConns:        maxIdleConnections,
		MaxIdleConnsPerHost: maxIdleConnectionsPerHost,
		IdleConnTimeout:     idleConnectionTimeout,
		TLSHandshakeTimeout: tlsHandshakeTimeout,
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

func (fetcher *WebFetcher) Close() error {
	if fetcher.cacheStore == nil {
		return nil
	}
	return fetcher.cacheStore.Close()
}

func (fetcher *WebFetcher) Fetch(rawURL string) *HTTPResponse {
	requestURL := withDefaultScheme(strings.TrimSpace(rawURL))
	if requestURL == "" {
		return &HTTPResponse{Error: "empty URL"}
	}
	if cached, found := fetcher.responses.Load(requestURL); found {
		return cached.(*HTTPResponse)
	}
	if !fetcher.settings.ForceRefresh {
		if stored, found := fetcher.loadStoredResponse(requestURL); found {
			fetcher.responses.Store(requestURL, stored)
			return stored
		}
	}
	if fetcher.settings.Offline {
		return &HTTPResponse{URL: requestURL, Error: "offline mode: cache miss"}
	}
	response := fetcher.download(requestURL)
	fetcher.responses.Store(requestURL, response)
	fetcher.storeResponse(response)
	return response
}

func (fetcher *WebFetcher) LookupAddresses(hostOrURL string) []string {
	return fetcher.lookupDNS(addressRecordKind, hostOrURL, net.DefaultResolver.LookupHost)
}

func (fetcher *WebFetcher) LookupTXTRecords(hostOrURL string) []string {
	return fetcher.lookupDNS(textRecordKind, hostOrURL, net.DefaultResolver.LookupTXT)
}

func withDefaultScheme(rawURL string) string {
	if rawURL == "" || hasHTTPScheme(rawURL) {
		return rawURL
	}
	return "https://" + rawURL
}

func hasHTTPScheme(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func (fetcher *WebFetcher) isFresh(fetchedAtUnix int64) bool {
	age := time.Since(time.Unix(fetchedAtUnix, 0))
	return age <= fetcher.settings.CacheTTL
}

func (fetcher *WebFetcher) loadStoredResponse(requestURL string) (*HTTPResponse, bool) {
	var (
		response    = HTTPResponse{URL: requestURL}
		headersJSON string
	)
	if fetcher.cacheStore == nil {
		return nil, false
	}
	err := fetcher.cacheStore.QueryRow(
		`SELECT final_url, status_code, headers_json, body, response_ms, fetched_at, error_text
		 FROM http_cache WHERE url = ?`, requestURL,
	).Scan(&response.FinalURL, &response.StatusCode, &headersJSON, &response.Body,
		&response.ResponseTimeMS, &response.FetchedAtUnix, &response.Error)
	if err != nil || !fetcher.isFresh(response.FetchedAtUnix) {
		return nil, false
	}
	response.Headers = map[string][]string{}
	_ = json.Unmarshal([]byte(headersJSON), &response.Headers)
	return &response, true
}

func (fetcher *WebFetcher) storeResponse(response *HTTPResponse) {
	if fetcher.cacheStore == nil {
		return
	}
	headersJSON, _ := json.Marshal(response.Headers)
	_, _ = fetcher.cacheStore.Exec(
		`INSERT OR REPLACE INTO http_cache
		 (url, final_url, status_code, headers_json, body, response_ms, fetched_at, error_text)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		response.URL, response.FinalURL, response.StatusCode, string(headersJSON),
		response.Body, response.ResponseTimeMS, response.FetchedAtUnix, response.Error,
	)
}

func (fetcher *WebFetcher) download(requestURL string) *HTTPResponse {
	startedAt := time.Now()
	response := &HTTPResponse{URL: requestURL, FetchedAtUnix: startedAt.Unix()}

	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		response.Error = err.Error()
		return response
	}
	request.Header.Set("User-Agent", fetcher.settings.UserAgent)
	request.Header.Set("Accept", acceptHeaderValue)

	reply, err := fetcher.client.Do(request)
	response.ResponseTimeMS = time.Since(startedAt).Milliseconds()
	if err != nil {
		response.Error = err.Error()
		return response
	}
	defer reply.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(reply.Body, fetcher.maxBodyBytes()))
	if readErr != nil {
		response.Error = readErr.Error()
	}
	response.StatusCode = reply.StatusCode
	response.Body = string(body)
	response.Headers = lowercaseHeaderNames(reply.Header)
	response.FinalURL = finalRequestURL(reply, requestURL)
	return response
}

func (fetcher *WebFetcher) maxBodyBytes() int64 {
	if fetcher.settings.MaxBodyBytes <= 0 {
		return defaultMaxBodyBytes
	}
	return fetcher.settings.MaxBodyBytes
}

func lowercaseHeaderNames(headers http.Header) map[string][]string {
	lowercased := make(map[string][]string, len(headers))
	for name, values := range headers {
		lowercased[strings.ToLower(name)] = values
	}
	return lowercased
}

func finalRequestURL(reply *http.Response, requestURL string) string {
	if reply.Request == nil || reply.Request.URL == nil {
		return requestURL
	}
	return reply.Request.URL.String()
}

func extractHost(hostOrURL string) string {
	input := strings.TrimSpace(hostOrURL)
	if input == "" {
		return ""
	}
	if strings.Contains(input, "://") {
		parsed, err := url.Parse(input)
		if err != nil {
			return ""
		}
		return strings.ToLower(parsed.Hostname())
	}
	if slash := strings.IndexByte(input, '/'); slash != -1 {
		input = input[:slash]
	}
	if host, _, err := net.SplitHostPort(input); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(input)
}

func (fetcher *WebFetcher) lookupDNS(kind, hostOrURL string, resolve func(context.Context, string) ([]string, error)) []string {
	host := extractHost(hostOrURL)
	if host == "" {
		return nil
	}
	lookupKey := kind + ":" + host
	if cached, found := fetcher.dnsRecords.Load(lookupKey); found {
		return cached.([]string)
	}
	if !fetcher.settings.ForceRefresh {
		if stored, found := fetcher.loadStoredDNS(lookupKey); found {
			fetcher.dnsRecords.Store(lookupKey, stored)
			return stored
		}
	}
	if fetcher.settings.Offline {
		return nil
	}
	lookupContext, cancel := context.WithTimeout(context.Background(), dnsLookupTimeout)
	defer cancel()
	records, err := resolve(lookupContext, host)
	if err != nil {
		var dnsError *net.DNSError
		if errors.As(err, &dnsError) && dnsError.IsNotFound {
			fetcher.dnsRecords.Store(lookupKey, []string(nil))
		}
		return nil
	}
	fetcher.dnsRecords.Store(lookupKey, records)
	fetcher.storeDNS(lookupKey, records)
	return records
}

func (fetcher *WebFetcher) loadStoredDNS(lookupKey string) ([]string, bool) {
	var (
		recordsJSON   string
		fetchedAtUnix int64
		records       []string
	)
	if fetcher.cacheStore == nil {
		return nil, false
	}
	err := fetcher.cacheStore.QueryRow(
		`SELECT records_json, fetched_at FROM dns_cache WHERE lookup_key = ?`, lookupKey,
	).Scan(&recordsJSON, &fetchedAtUnix)
	if err != nil || !fetcher.isFresh(fetchedAtUnix) {
		return nil, false
	}
	if json.Unmarshal([]byte(recordsJSON), &records) != nil {
		return nil, false
	}
	return records, true
}

func (fetcher *WebFetcher) storeDNS(lookupKey string, records []string) {
	if fetcher.cacheStore == nil {
		return
	}
	recordsJSON, _ := json.Marshal(records)
	_, _ = fetcher.cacheStore.Exec(
		`INSERT OR REPLACE INTO dns_cache (lookup_key, records_json, fetched_at) VALUES (?, ?, ?)`,
		lookupKey, string(recordsJSON), time.Now().Unix(),
	)
}
