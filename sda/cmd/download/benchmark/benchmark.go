// Package main provides benchmark modes for comparing download service endpoints.
//
// When running in a container (via docker compose), the tool auto-configures by:
// - Reading token from /shared/token
// - Reading public key from /shared/c4gh.pub.pem
// - Discovering an accessible dataset+file via the NEW service (/datasets/*)
// - Using container hostnames (download, download-new) for service URLs
//
// Supported modes:
//   - endpoint-e2e: compare the public download endpoints as exposed today
//     NEW: GET /files/{stable_id} with header X-C4GH-Public-Key
//     OLD: GET /s3/{dataset}/{path} with header Client-Public-Key
//   - validated-payload: run the same endpoint comparison, but first verify that
//     both responses decrypt to the same plaintext using the benchmark private key
//
// Usage (containerized - recommended):
//
//	docker compose -f .github/integration/sda-s3-integration.yml --profile benchmark run --rm benchmark
//
// Usage (local development):
//
//	go run benchmark.go -old http://localhost:8085 -new http://localhost:8087 \
//	    -token "Bearer xxx" -pubkey "xxx"
//
// Environment variables (override defaults when running in container):
//
//	OLD_URL, NEW_URL, ITERATIONS, REQUESTS, CONCURRENCY
//	FILE_ID, FILE_DATASET, FILE_PATH, TOKEN, PUBKEY
//	BENCHMARK_MODE, VERIFY_PRIVATE_KEY_PATH, VERIFY_PRIVATE_KEY_PASSPHRASE
package main

import (
	"cmp"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/neicnordic/crypt4gh/keys"
	crypt4ghstreaming "github.com/neicnordic/crypt4gh/streaming"
)

type BenchmarkMode string

const (
	ModeEndpointE2E      BenchmarkMode = "endpoint-e2e"
	ModeValidatedPayload BenchmarkMode = "validated-payload"
)

// Config holds benchmark configuration.
type Config struct {
	OldURL string
	NewURL string

	// Stable file ID (used for NEW /file/{id} endpoint).
	FileID string

	// Dataset ID + object path (used for OLD /s3/{dataset}/{path} endpoint).
	DatasetID string
	S3Path    string

	// Optional header used by the OLD service when configured with token-clientversion middleware.
	OldClientVersion string

	Token      string
	PublicKey  string
	Iterations int
	Requests   int

	Concurrency int
	Timeout     time.Duration
	SkipOld     bool
	SkipNew     bool
	OutputJSON  bool
	Mode        BenchmarkMode

	VerifyPrivateKeyPath       string
	VerifyPrivateKeyPassphrase string
}

// RequestResult holds the result of a single request.
type RequestResult struct {
	Duration   time.Duration
	StatusCode int
	Bytes      int64
	Error      error
}

// BenchmarkResult holds aggregated results for a benchmark run.
type BenchmarkResult struct {
	Name         string
	TotalTime    time.Duration
	Requests     int
	Successful   int
	Failed       int
	BytesTotal   int64
	Latencies    []time.Duration
	RequestsPerS float64
	Throughput   float64 // MB/s

	// StatusCounts includes both success and failure HTTP status codes.
	StatusCounts map[int]int
}

// Stats holds statistical measures.
type Stats struct {
	Min    time.Duration
	Max    time.Duration
	Mean   time.Duration
	StdDev time.Duration
	P50    time.Duration
	P90    time.Duration
	P95    time.Duration
	P99    time.Duration
}

// ComparisonResult holds results from multiple iterations.
type ComparisonResult struct {
	Mode       BenchmarkMode
	Old        []BenchmarkResult
	New        []BenchmarkResult
	OldSummary SummaryStats
	NewSummary SummaryStats
}

type payloadDigest struct {
	EncryptedBytes  int64
	PlaintextBytes  int64
	PlaintextSHA256 string
}

type payloadValidationResult struct {
	Old payloadDigest
	New payloadDigest
}

type payloadValidator struct {
	privateKey [32]byte
}

// SummaryStats holds summary statistics across iterations.
type SummaryStats struct {
	Name               string
	Iterations         int
	AvgRequestsPerS    float64
	StdDevRequestsPerS float64
	AvgLatency         Stats
	AvgThroughput      float64
	SuccessRate        float64
}

func main() {
	cfg := parseFlags()

	if err := autoDiscoverIfNeeded(&cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[auto-config] %v\n", err)
		// If we still don't have required values, fail early so the root cause is visible.
		needNew := !cfg.SkipNew && cfg.FileID == ""
		needOld := !cfg.SkipOld && (cfg.DatasetID == "" || cfg.S3Path == "")
		if needNew || needOld {
			os.Exit(1)
		}
	}

	if err := validateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Define targets
	oldTarget := Target{
		Name:            "old",
		BaseURL:         cfg.OldURL,
		PublicKeyHeader: "Client-Public-Key",
		BuildURL: func(cfg Config) (string, error) {
			if cfg.DatasetID == "" || cfg.S3Path == "" {
				return "", errors.New("dataset and path are required for old implementation")
			}
			ds := url.PathEscape(cfg.DatasetID)
			p := url.PathEscape(cfg.S3Path)

			return strings.TrimRight(cfg.OldURL, "/") + "/s3/" + ds + "/" + p, nil
		},
	}
	newTarget := Target{
		Name:            "new",
		BaseURL:         cfg.NewURL,
		PublicKeyHeader: "X-C4GH-Public-Key",
		BuildURL: func(cfg Config) (string, error) {
			if cfg.FileID == "" {
				return "", errors.New("file ID is required for new implementation")
			}
			id := url.PathEscape(cfg.FileID)

			return strings.TrimRight(cfg.NewURL, "/") + "/files/" + id, nil
		},
	}

	fmt.Println("=" + strings.Repeat("=", 70))
	fmt.Printf(" Download Service Benchmark (%s)\n", cfg.Mode)
	fmt.Println("=" + strings.Repeat("=", 70))
	fmt.Println("\nConfiguration:")
	fmt.Printf("  Mode:          %s\n", cfg.Mode)
	fmt.Printf("  Iterations:    %d\n", cfg.Iterations)
	fmt.Printf("  Requests:      %d per iteration\n", cfg.Requests)
	fmt.Printf("  Concurrency:   %d\n", cfg.Concurrency)
	fmt.Printf("  File ID:       %s\n", cfg.FileID)
	fmt.Printf("  Dataset ID:    %s\n", cfg.DatasetID)
	fmt.Printf("  OLD S3 Path:   %s\n", cfg.S3Path)
	if !cfg.SkipOld {
		fmt.Printf("  Old URL:       %s\n", cfg.OldURL)
	}
	if !cfg.SkipNew {
		fmt.Printf("  New URL:       %s\n", cfg.NewURL)
	}
	fmt.Println()

	client, err := newHTTPClient(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Client setup error: %v\n", err)
		os.Exit(1)
	}

	// Preflight (fail fast)
	if !cfg.SkipOld {
		if err := preflight(client, oldTarget, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Preflight OLD failed: %v\n", err)
			os.Exit(1)
		}
	}
	if !cfg.SkipNew {
		if err := preflight(client, newTarget, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Preflight NEW failed: %v\n", err)
			os.Exit(1)
		}
	}

	if cfg.Mode == ModeValidatedPayload {
		validation, err := validatePayloadEquivalence(client, cfg, oldTarget, newTarget)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Payload validation failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr,
			"[validation] plaintext match OK (bytes=%d, sha256=%s, encrypted-bytes old=%d new=%d)\n",
			validation.Old.PlaintextBytes,
			validation.Old.PlaintextSHA256,
			validation.Old.EncryptedBytes,
			validation.New.EncryptedBytes,
		)
	}

	result := runBenchmarks(client, cfg, oldTarget, newTarget)

	// Calculate and print summary
	if !cfg.SkipOld {
		result.OldSummary = calculateSummary("OLD", result.Old)
	}
	if !cfg.SkipNew {
		result.NewSummary = calculateSummary("NEW", result.New)
	}

	printSummary(result, cfg)

	if cfg.OutputJSON {
		printJSONOutput(result)
	}
}

func parseFlags() Config {
	cfg := Config{}

	// Load defaults from environment (for container mode)
	defaults := loadEnvironmentDefaults()

	flag.StringVar(&cfg.OldURL, "old", defaults.OldURL, "Base URL for old implementation (e.g., http://localhost:8085)")
	flag.StringVar(&cfg.NewURL, "new", defaults.NewURL, "Base URL for new implementation (e.g., http://localhost:8087)")

	flag.StringVar(&cfg.FileID, "file", defaults.FileID, "Stable file ID to download (NEW: /file/{id})")
	flag.StringVar(&cfg.DatasetID, "dataset", defaults.DatasetID, "Dataset ID (OLD: /s3/{dataset}/{path})")
	flag.StringVar(&cfg.S3Path, "path", defaults.S3Path, "Object path within dataset (OLD: /s3/{dataset}/{path}); usually submission path without user-prefix")
	flag.StringVar(&cfg.OldClientVersion, "old-client-version", defaults.OldClientVersion, "SDA-Client-Version header value for OLD service (when configured with token-clientversion)")

	flag.StringVar(&cfg.Token, "token", defaults.Token, "Authorization token (including 'Bearer ' prefix)")
	flag.StringVar(&cfg.PublicKey, "pubkey", defaults.PublicKey, "Public key for re-encryption (base64)")
	flag.IntVar(&cfg.Iterations, "iterations", defaults.Iterations, "Number of benchmark iterations")
	flag.IntVar(&cfg.Requests, "requests", defaults.Requests, "Number of requests per iteration")
	flag.IntVar(&cfg.Concurrency, "concurrency", defaults.Concurrency, "Number of concurrent requests")
	flag.DurationVar(&cfg.Timeout, "timeout", defaults.Timeout, "Request timeout")
	flag.BoolVar(&cfg.SkipOld, "skip-old", defaults.SkipOld, "Skip benchmarking old implementation")
	flag.BoolVar(&cfg.SkipNew, "skip-new", defaults.SkipNew, "Skip benchmarking new implementation")
	flag.BoolVar(&cfg.OutputJSON, "json", false, "Output results as JSON")
	flag.Func("mode", "Benchmark mode: endpoint-e2e or validated-payload", func(val string) error {
		mode, err := parseBenchmarkMode(val)
		if err != nil {
			return err
		}
		cfg.Mode = mode

		return nil
	})
	flag.StringVar(&cfg.VerifyPrivateKeyPath, "verify-private-key", defaults.VerifyPrivateKeyPath, "Private key path used to decrypt responses in validated-payload mode")
	flag.StringVar(&cfg.VerifyPrivateKeyPassphrase, "verify-private-key-passphrase", defaults.VerifyPrivateKeyPassphrase, "Private key passphrase used in validated-payload mode")

	flag.Parse() //nolint:revive // deep-exit: called only from main()

	if cfg.Mode == "" {
		cfg.Mode = defaults.Mode
	}

	return cfg
}

// loadEnvironmentDefaults loads configuration from environment variables and files.
// This enables auto-configuration when running in a Docker container.
func loadEnvironmentDefaults() Config {
	cfg := Config{
		OldURL:                     getEnv("OLD_URL", ""),
		NewURL:                     getEnv("NEW_URL", ""),
		FileID:                     getEnv("FILE_ID", ""),
		DatasetID:                  getEnv("FILE_DATASET", ""),
		S3Path:                     getEnv("FILE_PATH", ""),
		OldClientVersion:           getEnv("OLD_CLIENT_VERSION", "v0.2.0"),
		Iterations:                 getEnvInt("ITERATIONS", 5),
		Requests:                   getEnvInt("REQUESTS", 100),
		Concurrency:                getEnvInt("CONCURRENCY", 10),
		Timeout:                    30 * time.Second,
		SkipOld:                    getEnvBool("SKIP_OLD", false),
		SkipNew:                    getEnvBool("SKIP_NEW", false),
		Mode:                       mustParseBenchmarkMode(getEnv("BENCHMARK_MODE", string(ModeEndpointE2E))),
		VerifyPrivateKeyPath:       getEnv("VERIFY_PRIVATE_KEY_PATH", "/shared/c4gh.sec.pem"),
		VerifyPrivateKeyPassphrase: getEnv("VERIFY_PRIVATE_KEY_PASSPHRASE", "c4ghpass"),
	}

	// Try to read token from /shared/token (container mode)
	if token, err := os.ReadFile("/shared/token"); err == nil {
		cfg.Token = "Bearer " + strings.TrimSpace(string(token))
		fmt.Fprintln(os.Stderr, "[auto-config] Loaded token from /shared/token")
	} else if envToken := os.Getenv("TOKEN"); envToken != "" {
		cfg.Token = envToken
	}

	// Try to read public key from /shared/c4gh.pub.pem (container mode)
	if pubkey, err := os.ReadFile("/shared/c4gh.pub.pem"); err == nil {
		cfg.PublicKey = base64.StdEncoding.EncodeToString(pubkey)
		fmt.Fprintln(os.Stderr, "[auto-config] Loaded public key from /shared/c4gh.pub.pem")
	} else if envPubKey := os.Getenv("PUBKEY"); envPubKey != "" {
		cfg.PublicKey = envPubKey
	}

	return cfg
}

func parseBenchmarkMode(val string) (BenchmarkMode, error) {
	switch BenchmarkMode(strings.TrimSpace(val)) {
	case ModeEndpointE2E:
		return ModeEndpointE2E, nil
	case ModeValidatedPayload:
		return ModeValidatedPayload, nil
	default:
		return "", fmt.Errorf("unsupported benchmark mode %q", val)
	}
}

func mustParseBenchmarkMode(val string) BenchmarkMode {
	mode, err := parseBenchmarkMode(val)
	if err != nil {
		return ModeEndpointE2E
	}

	return mode
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}

	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}

	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		return val == "true" || val == "1" || val == "yes"
	}

	return defaultVal
}

type Target struct {
	Name            string
	BaseURL         string
	PublicKeyHeader string
	BuildURL        func(cfg Config) (string, error)
}

type discoveredFile struct {
	FileID   string `json:"fileId"`
	FilePath string `json:"filePath"`
}

func autoDiscoverIfNeeded(cfg *Config) error {
	if cfg == nil {
		return errors.New("nil config")
	}

	needNew := !cfg.SkipNew && cfg.FileID == ""
	needOld := !cfg.SkipOld && (cfg.DatasetID == "" || cfg.S3Path == "")

	if !needNew && !needOld {
		return nil
	}

	if cfg.NewURL == "" {
		return errors.New("cannot auto-discover without NEW_URL (or -new)")
	}
	if cfg.Token == "" {
		return errors.New("cannot auto-discover without token")
	}

	caseInfo, err := discoverBenchmarkCase(*cfg)
	if err != nil {
		return err
	}

	if cfg.FileID == "" {
		cfg.FileID = caseInfo.FileID
		fmt.Fprintf(os.Stderr, "[auto-config] Discovered file ID via new service: %s\n", cfg.FileID)
	}
	if cfg.DatasetID == "" {
		cfg.DatasetID = caseInfo.DatasetID
		fmt.Fprintf(os.Stderr, "[auto-config] Discovered dataset ID via new service: %s\n", cfg.DatasetID)
	}
	if cfg.S3Path == "" {
		cfg.S3Path = caseInfo.S3Path
		fmt.Fprintf(os.Stderr, "[auto-config] Derived old S3 path from submission path: %s\n", cfg.S3Path)
	}

	return nil
}

type benchmarkCase struct {
	DatasetID      string
	FileID         string
	SubmissionPath string
	S3Path         string
}

func discoverBenchmarkCase(cfg Config) (*benchmarkCase, error) {
	if cfg.NewURL == "" {
		return nil, errors.New("cannot auto-discover without NEW_URL (or -new)")
	}
	if cfg.Token == "" {
		return nil, errors.New("cannot auto-discover without token")
	}
	if !cfg.SkipOld {
		if cfg.OldURL == "" {
			return nil, errors.New("cannot auto-discover a comparable file without OLD_URL (or -old)")
		}
		if cfg.PublicKey == "" {
			return nil, errors.New("cannot auto-discover a comparable file without public key")
		}
	}

	client := &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			DisableCompression: true,
			TLSClientConfig:    &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // benchmark tool, not production
		},
	}

	newBase := strings.TrimRight(cfg.NewURL, "/")

	// 1) list datasets from NEW service
	datasetsURL := newBase + "/datasets"
	resp, err := doJSONRequest(client, datasetsURL, cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("discover datasets: %w", err)
	}
	var datasetsResp struct {
		Datasets []string `json:"datasets"`
	}
	if err := json.Unmarshal(resp, &datasetsResp); err != nil {
		return nil, fmt.Errorf("discover datasets: invalid JSON: %w", err)
	}
	if len(datasetsResp.Datasets) == 0 {
		return nil, errors.New("discover datasets: no datasets returned (DB likely not seeded yet; run the integration tests to create/map datasets before benchmarking)")
	}

	// 2) try datasets until we find one that also works against OLD
	var lastOldStatus int
	for _, d := range datasetsResp.Datasets {
		if d == "" {
			continue
		}
		filesURL := newBase + "/datasets/" + url.PathEscape(d) + "/files"
		resp, err := doJSONRequest(client, filesURL, cfg.Token)
		if err != nil {
			continue
		}
		var filesResp struct {
			Files []discoveredFile `json:"files"`
		}
		if err := json.Unmarshal(resp, &filesResp); err != nil {
			continue
		}
		if len(filesResp.Files) == 0 {
			continue
		}

		// Try first few files in the dataset
		maxFiles := 5
		if len(filesResp.Files) < maxFiles {
			maxFiles = len(filesResp.Files)
		}
		for i := 0; i < maxFiles; i++ {
			f := filesResp.Files[i]
			if f.FileID == "" {
				continue
			}
			s3Path, err := deriveOldS3Path(f.FilePath)
			if err != nil {
				continue
			}
			candidate := &benchmarkCase{DatasetID: d, FileID: f.FileID, SubmissionPath: f.FilePath, S3Path: s3Path}

			if cfg.SkipOld {
				return candidate, nil
			}

			status, err := tryOldPreflight(client, cfg, candidate)
			lastOldStatus = status
			if err == nil {
				return candidate, nil
			}
		}
	}

	if cfg.SkipOld {
		return nil, errors.New("failed to discover any file from NEW datasets")
	}

	return nil, fmt.Errorf("no discovered NEW dataset/file was accessible via OLD (last status=%d). This usually means the OLD service authorisation (visas) does not grant access to the datasets returned by the NEW service", lastOldStatus)
}

func tryOldPreflight(client *http.Client, cfg Config, c *benchmarkCase) (int, error) {
	ds := url.PathEscape(c.DatasetID)
	p := url.PathEscape(c.S3Path)
	u := strings.TrimRight(cfg.OldURL, "/") + "/s3/" + ds + "/" + p

	extra := map[string]string{
		"Range": "bytes=0-0",
	}
	if cfg.OldClientVersion != "" {
		extra["SDA-Client-Version"] = cfg.OldClientVersion
	}

	res := makeRequestWithExtras(client, u, cfg.Token, cfg.PublicKey, "Client-Public-Key", extra)
	if res.Error != nil {
		return 0, res.Error
	}
	if res.StatusCode >= 400 {
		return res.StatusCode, fmt.Errorf("old returned %d", res.StatusCode)
	}

	return res.StatusCode, nil
}

func doJSONRequest(client *http.Client, urlStr, token string) ([]byte, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", token)

	resp, err := client.Do(req) //nolint:gosec // benchmark tool, URLs from CLI flags
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s returned %d: %s", urlStr, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return body, nil
}

func deriveOldS3Path(submittedPath string) (string, error) {
	p := strings.TrimPrefix(submittedPath, "/")
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("empty submission path")
	}
	parts := strings.SplitN(p, "/", 2)
	if len(parts) == 2 {
		p = parts[1]
	} else {
		p = parts[0]
	}
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("could not derive old S3 path from submission path %q", submittedPath)
	}
	if !strings.HasSuffix(p, ".c4gh") {
		p += ".c4gh"
	}

	return p, nil
}

func validateConfig(cfg Config) error {
	if cfg.SkipOld && cfg.SkipNew {
		return errors.New("cannot skip both old and new implementations")
	}
	if cfg.Token == "" {
		return errors.New("-token is required (or set TOKEN or mount /shared/token)")
	}
	if cfg.PublicKey == "" {
		return errors.New("-pubkey is required (or set PUBKEY or mount /shared/c4gh.pub.pem)")
	}
	if !cfg.SkipOld && cfg.OldURL == "" {
		return errors.New("-old URL is required (or use -skip-old)")
	}
	if !cfg.SkipNew && cfg.NewURL == "" {
		return errors.New("-new URL is required (or use -skip-new)")
	}
	if !cfg.SkipNew && cfg.FileID == "" {
		return errors.New("file ID is required for new implementation (use -file or set FILE_ID)")
	}
	if !cfg.SkipOld {
		if cfg.DatasetID == "" {
			return errors.New("dataset ID is required for old implementation (use -dataset or set FILE_DATASET)")
		}
		if cfg.S3Path == "" {
			return errors.New("path is required for old implementation (use -path or set FILE_PATH)")
		}
	}
	if cfg.Iterations < 1 {
		return errors.New("-iterations must be at least 1")
	}
	if cfg.Requests < 1 {
		return errors.New("-requests must be at least 1")
	}
	if cfg.Concurrency < 1 {
		return errors.New("-concurrency must be at least 1")
	}
	if _, err := parseBenchmarkMode(string(cfg.Mode)); err != nil {
		return err
	}
	if cfg.Mode == ModeValidatedPayload {
		if cfg.SkipOld || cfg.SkipNew {
			return errors.New("validated-payload mode requires both old and new implementations")
		}
		if cfg.VerifyPrivateKeyPath == "" {
			return errors.New("-verify-private-key is required in validated-payload mode")
		}
	}

	return nil
}

func newHTTPClient(cfg Config) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}

	return &http.Client{
		Timeout: cfg.Timeout,
		Jar:     jar,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: cfg.Concurrency,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // benchmark tool, not production
			DisableCompression:  true,
		},
	}, nil
}

func preflight(client *http.Client, target Target, cfg Config) error {
	u, err := target.BuildURL(cfg)
	if err != nil {
		return err
	}
	extra := map[string]string{
		"Range": "bytes=0-0",
	}
	if target.Name == "old" && cfg.OldClientVersion != "" {
		extra["SDA-Client-Version"] = cfg.OldClientVersion
	}
	res := makeRequestWithExtras(client, u, cfg.Token, cfg.PublicKey, target.PublicKeyHeader, extra)
	if res.Error != nil {
		return res.Error
	}
	if res.StatusCode >= 400 {
		return fmt.Errorf("%s preflight returned %d", target.Name, res.StatusCode)
	}
	if res.Bytes <= 0 {
		// not necessarily fatal, but almost always indicates we’re not benchmarking a real file
		fmt.Fprintf(os.Stderr, "[warn] %s preflight read %d bytes\n", target.Name, res.Bytes)
	}
	fmt.Fprintf(os.Stderr, "[preflight] %s OK (status=%d, bytes=%d)\n", strings.ToUpper(target.Name), res.StatusCode, res.Bytes)

	return nil
}

func runBenchmark(client *http.Client, target Target, cfg Config) BenchmarkResult {
	u, err := target.BuildURL(cfg)
	if err != nil {
		return BenchmarkResult{Name: target.Name, Requests: cfg.Requests, Failed: cfg.Requests, StatusCounts: map[int]int{0: cfg.Requests}}
	}

	results := make([]RequestResult, cfg.Requests)
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, cfg.Concurrency)

	startTime := time.Now()

	for i := 0; i < cfg.Requests; i++ {
		wg.Go(func() {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			var extra map[string]string
			if target.Name == "old" && cfg.OldClientVersion != "" {
				extra = map[string]string{"SDA-Client-Version": cfg.OldClientVersion}
			}
			results[i] = makeRequestWithExtras(client, u, cfg.Token, cfg.PublicKey, target.PublicKeyHeader, extra)
		})
	}

	wg.Wait()
	totalTime := time.Since(startTime)

	return aggregateResults(target.Name, results, totalTime)
}

func buildRequest(urlStr, token, publicKey, publicKeyHeader string, extraHeaders map[string]string) (*http.Request, error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", token)
	req.Header.Set(publicKeyHeader, publicKey)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	return req, nil
}

func makeRequestWithExtras(client *http.Client, urlStr, token, publicKey, publicKeyHeader string, extraHeaders map[string]string) RequestResult {
	req, err := buildRequest(urlStr, token, publicKey, publicKeyHeader, extraHeaders)
	if err != nil {
		return RequestResult{Error: err}
	}

	start := time.Now()
	resp, err := client.Do(req) //nolint:gosec // benchmark tool, URLs from CLI flags
	if err != nil {
		return RequestResult{Duration: time.Since(start), Error: err}
	}
	defer resp.Body.Close()

	bytesRead, _ := io.Copy(io.Discard, resp.Body)
	duration := time.Since(start)

	return RequestResult{
		Duration:   duration,
		StatusCode: resp.StatusCode,
		Bytes:      bytesRead,
	}
}

func aggregateResults(name string, results []RequestResult, totalTime time.Duration) BenchmarkResult {
	br := BenchmarkResult{
		Name:         name,
		TotalTime:    totalTime,
		Requests:     len(results),
		StatusCounts: make(map[int]int),
	}

	for _, r := range results {
		code := r.StatusCode
		if r.Error != nil {
			code = 0
		}
		br.StatusCounts[code]++

		if r.Error != nil || r.StatusCode >= 400 {
			br.Failed++

			continue
		}

		br.Successful++
		br.Latencies = append(br.Latencies, r.Duration)
		br.BytesTotal += r.Bytes
	}

	if totalTime > 0 {
		br.RequestsPerS = float64(br.Successful) / totalTime.Seconds()
		br.Throughput = float64(br.BytesTotal) / totalTime.Seconds() / 1024 / 1024 // MB/s
	}

	return br
}

func calculateStats(latencies []time.Duration) Stats {
	if len(latencies) == 0 {
		return Stats{}
	}

	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	slices.SortFunc(sorted, cmp.Compare)

	var sum time.Duration
	for _, l := range sorted {
		sum += l
	}
	mean := time.Duration(int64(sum) / int64(len(sorted)))

	var variance float64
	for _, l := range sorted {
		diff := float64(l - mean)
		variance += diff * diff
	}
	stdDev := time.Duration(math.Sqrt(variance / float64(len(sorted))))

	return Stats{
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
		Mean:   mean,
		StdDev: stdDev,
		P50:    percentile(sorted, 50),
		P90:    percentile(sorted, 90),
		P95:    percentile(sorted, 95),
		P99:    percentile(sorted, 99),
	}
}

func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * float64(p) / 100)

	return sorted[idx]
}

func calculateSummary(name string, results []BenchmarkResult) SummaryStats {
	summary := SummaryStats{
		Name:       name,
		Iterations: len(results),
	}

	if len(results) == 0 {
		return summary
	}

	var totalRPS, totalThroughput float64
	var totalSuccessful, totalRequests int
	var allLatencies []time.Duration

	for _, r := range results {
		totalRPS += r.RequestsPerS
		totalThroughput += r.Throughput
		totalSuccessful += r.Successful
		totalRequests += r.Requests
		allLatencies = append(allLatencies, r.Latencies...)
	}

	n := float64(len(results))
	summary.AvgRequestsPerS = totalRPS / n
	summary.AvgThroughput = totalThroughput / n
	summary.SuccessRate = float64(totalSuccessful) / float64(totalRequests) * 100
	summary.AvgLatency = calculateStats(allLatencies)

	// Calculate RPS standard deviation
	var variance float64
	for _, r := range results {
		diff := r.RequestsPerS - summary.AvgRequestsPerS
		variance += diff * diff
	}
	summary.StdDevRequestsPerS = math.Sqrt(variance / n)

	return summary
}

func printIterationResult(r BenchmarkResult) {
	stats := calculateStats(r.Latencies)
	fmt.Printf("    Requests:  %d successful, %d failed\n", r.Successful, r.Failed)
	if r.Failed > 0 {
		fmt.Printf("    Failures:  %s\n", formatStatusCounts(r.StatusCounts))
	}
	fmt.Printf("    RPS:       %.2f req/s\n", r.RequestsPerS)
	fmt.Printf("    Latency:   mean=%v, p95=%v, p99=%v\n", stats.Mean.Round(time.Millisecond), stats.P95.Round(time.Millisecond), stats.P99.Round(time.Millisecond))
	fmt.Printf("    Throughput: %.2f MB/s\n", r.Throughput)
}

func formatStatusCounts(m map[int]int) string {
	if len(m) == 0 {
		return "(none)"
	}
	type kv struct {
		Code  int
		Count int
	}
	pairs := make([]kv, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, kv{Code: k, Count: v})
	}
	slices.SortFunc(pairs, func(a, b kv) int { return b.Count - a.Count })

	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		label := fmt.Sprintf("%d", p.Code)
		if p.Code == 0 {
			label = "err"
		}
		parts = append(parts, fmt.Sprintf("%s=%d", label, p.Count))
	}

	return strings.Join(parts, ", ")
}

func runBenchmarks(client *http.Client, cfg Config, oldTarget, newTarget Target) *ComparisonResult {
	result := &ComparisonResult{Mode: cfg.Mode}

	switch {
	case !cfg.SkipOld && !cfg.SkipNew:
		fmt.Println("-" + strings.Repeat("-", 70))
		fmt.Printf(" Benchmarking paired endpoint runs (%s)\n", cfg.Mode)
		fmt.Println("-" + strings.Repeat("-", 70))

		for i := 1; i <= cfg.Iterations; i++ {
			order := pairedIterationTargets(i, oldTarget, newTarget)
			fmt.Printf("\n  Paired iteration %d/%d (%s -> %s)\n",
				i, cfg.Iterations, strings.ToUpper(order[0].Name), strings.ToUpper(order[1].Name))
			for _, target := range order {
				res := runBenchmark(client, target, cfg)
				appendBenchmarkResult(result, target.Name, res)
				fmt.Printf("  %s:\n", strings.ToUpper(target.Name))
				printIterationResult(res)
			}

			if i < cfg.Iterations {
				time.Sleep(2 * time.Second)
			}
		}
	case !cfg.SkipOld:
		fmt.Println("-" + strings.Repeat("-", 70))
		fmt.Println(" Benchmarking OLD endpoint")
		fmt.Println("-" + strings.Repeat("-", 70))
		for i := 1; i <= cfg.Iterations; i++ {
			fmt.Printf("\n  Iteration %d/%d...\n", i, cfg.Iterations)
			res := runBenchmark(client, oldTarget, cfg)
			result.Old = append(result.Old, res)
			printIterationResult(res)
			if i < cfg.Iterations {
				time.Sleep(2 * time.Second)
			}
		}
	case !cfg.SkipNew:
		fmt.Println("-" + strings.Repeat("-", 70))
		fmt.Println(" Benchmarking NEW endpoint")
		fmt.Println("-" + strings.Repeat("-", 70))
		for i := 1; i <= cfg.Iterations; i++ {
			fmt.Printf("\n  Iteration %d/%d...\n", i, cfg.Iterations)
			res := runBenchmark(client, newTarget, cfg)
			result.New = append(result.New, res)
			printIterationResult(res)
			if i < cfg.Iterations {
				time.Sleep(2 * time.Second)
			}
		}
	default:
		// Both benchmarks are being skipped; nothing to do
	}

	return result
}

func pairedIterationTargets(iteration int, oldTarget, newTarget Target) []Target {
	if iteration%2 == 1 {
		return []Target{oldTarget, newTarget}
	}

	return []Target{newTarget, oldTarget}
}

func appendBenchmarkResult(result *ComparisonResult, targetName string, res BenchmarkResult) {
	switch targetName {
	case "old":
		result.Old = append(result.Old, res)
	case "new":
		result.New = append(result.New, res)
	default:
		// Unknown target name; skip appending
	}
}

func validatePayloadEquivalence(client *http.Client, cfg Config, oldTarget, newTarget Target) (*payloadValidationResult, error) {
	validator, err := newPayloadValidator(cfg.VerifyPrivateKeyPath, cfg.VerifyPrivateKeyPassphrase)
	if err != nil {
		return nil, err
	}

	oldDigest, err := fetchPayloadDigest(client, oldTarget, cfg, validator)
	if err != nil {
		return nil, fmt.Errorf("old: %w", err)
	}
	newDigest, err := fetchPayloadDigest(client, newTarget, cfg, validator)
	if err != nil {
		return nil, fmt.Errorf("new: %w", err)
	}
	if err := comparePayloadDigests(oldDigest, newDigest); err != nil {
		return nil, err
	}

	return &payloadValidationResult{Old: oldDigest, New: newDigest}, nil
}

func newPayloadValidator(path, passphrase string) (*payloadValidator, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open verification private key %q: %w", path, err)
	}
	defer file.Close()

	var password []byte
	if passphrase != "" {
		password = []byte(passphrase)
	}

	privateKey, err := keys.ReadPrivateKey(file, password)
	if err != nil {
		return nil, fmt.Errorf("read verification private key %q: %w", path, err)
	}

	return &payloadValidator{privateKey: privateKey}, nil
}

func fetchPayloadDigest(client *http.Client, target Target, cfg Config, validator *payloadValidator) (payloadDigest, error) {
	u, err := target.BuildURL(cfg)
	if err != nil {
		return payloadDigest{}, err
	}

	var extraHeaders map[string]string
	if target.Name == "old" && cfg.OldClientVersion != "" {
		extraHeaders = map[string]string{"SDA-Client-Version": cfg.OldClientVersion}
	}

	req, err := buildRequest(u, cfg.Token, cfg.PublicKey, target.PublicKeyHeader, extraHeaders)
	if err != nil {
		return payloadDigest{}, err
	}

	resp, err := client.Do(req) //nolint:gosec // benchmark tool, URLs from CLI flags
	if err != nil {
		return payloadDigest{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))

		return payloadDigest{}, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return validator.digest(resp.Body)
}

func (v *payloadValidator) digest(body io.Reader) (payloadDigest, error) {
	counted := &countingReader{reader: body}
	reader, err := crypt4ghstreaming.NewCrypt4GHReader(counted, v.privateKey, nil)
	if err != nil {
		return payloadDigest{}, err
	}
	defer reader.Close()

	hash := sha256.New()
	plaintextBytes, err := io.Copy(hash, reader)
	if err != nil {
		return payloadDigest{}, err
	}

	return payloadDigest{
		EncryptedBytes:  counted.bytesRead,
		PlaintextBytes:  plaintextBytes,
		PlaintextSHA256: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func comparePayloadDigests(oldDigest, newDigest payloadDigest) error {
	if oldDigest.PlaintextBytes != newDigest.PlaintextBytes {
		return fmt.Errorf("plaintext byte length mismatch: old=%d new=%d", oldDigest.PlaintextBytes, newDigest.PlaintextBytes)
	}
	if oldDigest.PlaintextSHA256 != newDigest.PlaintextSHA256 {
		return fmt.Errorf("plaintext sha256 mismatch: old=%s new=%s", oldDigest.PlaintextSHA256, newDigest.PlaintextSHA256)
	}

	return nil
}

func percentChange(newValue, oldValue float64) float64 {
	switch {
	case oldValue == 0 && newValue == 0:
		return 0
	case oldValue == 0 && newValue > 0:
		return math.Inf(1)
	case oldValue == 0:
		return math.Inf(-1)
	default:
		return (newValue - oldValue) / oldValue * 100
	}
}

type countingReader struct {
	reader    io.Reader
	bytesRead int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead += int64(n)

	return n, err
}

func printSummary(result *ComparisonResult, cfg Config) {
	fmt.Println("\n" + "=" + strings.Repeat("=", 70))
	fmt.Printf(" SUMMARY (%s)\n", cfg.Mode)
	fmt.Println("=" + strings.Repeat("=", 70))

	if !cfg.SkipOld {
		printSummaryStats(result.OldSummary)
	}

	if !cfg.SkipNew {
		printSummaryStats(result.NewSummary)
	}

	// Print comparison if both were run
	if !cfg.SkipOld && !cfg.SkipNew {
		fmt.Println("\n" + "-" + strings.Repeat("-", 70))
		fmt.Println(" COMPARISON (NEW vs OLD)")
		fmt.Println("-" + strings.Repeat("-", 70))

		rpsChange := percentChange(result.NewSummary.AvgRequestsPerS, result.OldSummary.AvgRequestsPerS)
		latencyChange := percentChange(float64(result.NewSummary.AvgLatency.Mean), float64(result.OldSummary.AvgLatency.Mean))
		throughputChange := percentChange(result.NewSummary.AvgThroughput, result.OldSummary.AvgThroughput)

		fmt.Printf("\n  Requests/sec:  %+.1f%% ", rpsChange)
		printChangeIndicator(rpsChange, true)

		fmt.Printf("  Mean Latency:  %+.1f%% ", latencyChange)
		printChangeIndicator(latencyChange, false)

		fmt.Printf("  Throughput:    %+.1f%% ", throughputChange)
		printChangeIndicator(throughputChange, true)

		fmt.Println()

		// Verdict
		fmt.Println("\n  Verdict:")
		switch {
		case rpsChange > 5:
			fmt.Println("    NEW endpoint is FASTER")
		case rpsChange < -5:
			fmt.Println("    OLD endpoint is FASTER")
		default:
			fmt.Println("    Performance is SIMILAR (within 5%)")
		}
	}

	fmt.Println()
}

func printSummaryStats(s SummaryStats) {
	fmt.Printf("\n  %s Endpoint (%d iterations):\n", s.Name, s.Iterations)
	fmt.Printf("    Requests/sec:   %.2f (+/- %.2f)\n", s.AvgRequestsPerS, s.StdDevRequestsPerS)
	fmt.Printf("    Success rate:   %.1f%%\n", s.SuccessRate)
	fmt.Printf("    Throughput:     %.2f MB/s\n", s.AvgThroughput)
	fmt.Println("    Latency:")
	fmt.Printf("      Mean:   %v\n", s.AvgLatency.Mean.Round(time.Millisecond))
	fmt.Printf("      StdDev: %v\n", s.AvgLatency.StdDev.Round(time.Millisecond))
	fmt.Printf("      Min:    %v\n", s.AvgLatency.Min.Round(time.Millisecond))
	fmt.Printf("      P50:    %v\n", s.AvgLatency.P50.Round(time.Millisecond))
	fmt.Printf("      P90:    %v\n", s.AvgLatency.P90.Round(time.Millisecond))
	fmt.Printf("      P95:    %v\n", s.AvgLatency.P95.Round(time.Millisecond))
	fmt.Printf("      P99:    %v\n", s.AvgLatency.P99.Round(time.Millisecond))
	fmt.Printf("      Max:    %v\n", s.AvgLatency.Max.Round(time.Millisecond))
}

func printChangeIndicator(change float64, higherIsBetter bool) {
	good := (change > 0) == higherIsBetter

	switch {
	case math.Abs(change) < 5:
		fmt.Println("(~)")
	case good:
		fmt.Println("(better)")
	default:
		fmt.Println("(worse)")
	}
}

func printJSONOutput(result *ComparisonResult) {
	fmt.Println("\n" + "-" + strings.Repeat("-", 70))
	fmt.Println(" JSON Output")
	fmt.Println("-" + strings.Repeat("-", 70))

	output, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(output))
}
