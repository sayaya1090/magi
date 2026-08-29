package openai

import (
	"testing"
	"time"
)

// BaseURL answers what base() has always computed for the request itself, so a reader and a
// request can never disagree: the override when one is installed, the configured endpoint when
// none or when it is cleared.
func TestBaseURLReadsWhatARequestWouldUse(t *testing.T) {
	c := &Client{baseURL: "http://configured:11434/v1"}
	if got := c.BaseURL(); got != "http://configured:11434/v1" {
		t.Fatalf("no override means the configured endpoint, got %q", got)
	}
	tok := c.SetBaseURL("http://elsewhere:8000/v1")
	if tok == 0 {
		t.Fatal("a redirect that happened answers a token")
	}
	if got := c.BaseURL(); got != "http://elsewhere:8000/v1" {
		t.Fatalf("the override is where requests go now, got %q", got)
	}
	c.ClearBaseURL(tok)
	if got := c.BaseURL(); got != "http://configured:11434/v1" {
		t.Fatalf("cleared means back to configured, got %q", got)
	}
}

// The option constructors guard their zeroes: a zero cap or window is "provider default", not a
// cap of nothing.
func TestOptionsGuardTheirZeroes(t *testing.T) {
	c := &Client{}
	WithMaxTokens(0)(c)
	if c.maxTokens != 0 {
		t.Fatal("0 means provider default and must not be stored as a cap")
	}
	WithMaxTokens(4096)(c)
	if c.maxTokens != 4096 {
		t.Fatalf("a real cap is stored, got %d", c.maxTokens)
	}
	WithWindow(func(model string) int { return 128000 })(c)
	if c.window == nil || c.window("m") != 128000 {
		t.Fatal("the window resolver is the app's, injected — it must be held as given")
	}
	WithResponseHeaderTimeout(0)(c)
	if c.http != nil {
		t.Fatal("a non-positive timeout leaves the transport alone")
	}
	WithResponseHeaderTimeout(3 * time.Second)(c)
	if c.http == nil || c.http.Timeout != 0 {
		t.Fatal("the header timeout must not cap the stream body (client Timeout stays 0)")
	}
}
