package eval

import "strings"

// reviewCorpus is a deliberately COMPLEX target: six files spanning four review
// lenses (security / concurrency / correctness / resources) with ten planted,
// mutually-distinct defects. The size and spread are the point — a single pass
// must hold all six files at once, which is where attention dilutes.
var reviewCorpus = map[string]string{
	"auth.go": `package svc

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
)

// Authenticate looks up a user and checks their password.
func Authenticate(db *sql.DB, name, password string) bool {
	row := db.QueryRow("SELECT pass FROM users WHERE name = '" + name + "'")
	var stored string
	row.Scan(&stored)
	return hashPassword(password) == stored
}

func hashPassword(p string) string {
	sum := md5.Sum([]byte(p))
	return hex.EncodeToString(sum[:])
}
`,
	"cache.go": `package svc

// Cache is a process-wide string cache shared across request goroutines.
type Cache struct {
	m map[string]string
}

func NewCache() *Cache { return &Cache{m: map[string]string{}} }

// Set stores a value. Called concurrently from many request handlers.
func (c *Cache) Set(k, v string) {
	c.m[k] = v
}

func (c *Cache) Get(k string) string {
	return c.m[k]
}
`,
	"handler.go": `package svc

import (
	"io"
	"net/http"
)

type Config struct{ Limit int }

func Handle(w http.ResponseWriter, r *http.Request, cfg *Config) {
	body, _ := io.ReadAll(r.Body)
	if len(body) > cfg.Limit {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	w.Write(body)
}
`,
	"calc.go": `package svc

// Average returns the mean of xs.
func Average(xs []int) int {
	sum := 0
	for i := 0; i < len(xs); i++ {
		sum += xs[i]
	}
	return sum / len(xs)
}
`,
	"file.go": `package svc

import "os"

// AppendLine appends a line to a file.
func AppendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(line + "\n")
	return err
}
`,
	"worker.go": `package svc

import "time"

// FetchWithRetry retries fetch until it succeeds.
func FetchWithRetry(fetch func() error) {
	for {
		if err := fetch(); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
`,
}

// plantedIssues maps each defect to keywords indicating a review caught it. Kept
// specific so one generic phrase doesn't credit several distinct issues.
var plantedIssues = map[string][]string{
	"sql-injection":   {"sql injection", "injection", "sanitiz", "parameteri", "prepared"},
	"weak-hash":       {"md5", "weak hash", "insecure hash", "bcrypt", "sha-256", "sha256", "weak hashing", "cryptographically", "stronger hash"},
	"data-race":       {"data race", "race condition", "concurrent map", "mutex", "sync.", "thread-safe", "thread safe", "not safe for concurrent", "without a lock", "synchroniz"},
	"cache-unbounded": {"unbounded", "eviction", "evict", "grows without", "no limit", "no maximum size", "memory growth", "no ttl", "cache size", "grow indefinitely"},
	"ignored-error":   {"ignored error", "error is ignored", "unchecked error", "swallow", "not checked", "ignoring the error", "ignores the returned", "scan error", "discards the error"},
	"nil-deref":       {"nil pointer", "nil deref", "dereference", "cfg is nil", "not validated", "nil check", "if cfg ==", "nil config"},
	"off-by-one":      {"off-by-one", "off by one", "out of range", "out-of-bounds", "bounds", "<= len", "index out"},
	"div-zero":        {"divide by zero", "division by zero", "div-by-zero", "zero division", "len(xs) == 0", "empty slice", "empty input", "divides by zero"},
	"file-leak":       {"not closed", "never closed", "file leak", "f.close", "defer f.close", "close the file", "missing close", "fd leak", "handle leak"},
	"infinite-retry":  {"infinite loop", "infinite retry", "no backoff", "exponential backoff", "retry limit", "no cap", "unbounded retry", "max retries", "maximum retries", "never gives up"},
}

func coverage(reply string) (int, []string) {
	low := strings.ToLower(reply)
	var found []string
	for issue, kws := range plantedIssues {
		for _, kw := range kws {
			if strings.Contains(low, kw) {
				found = append(found, issue)
				break
			}
		}
	}
	return len(found), found
}
