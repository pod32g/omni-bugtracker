package egress

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIsBlockedIPCoversTheDangerousRanges(t *testing.T) {
	for _, s := range []string{
		"127.0.0.1",       // loopback
		"127.1.2.3",       // the whole /8, not just .0.1
		"0.0.0.0",         // "this host"
		"169.254.169.254", // cloud metadata — the one that matters most
		"10.1.2.3",
		"172.16.0.1",
		"172.31.255.255", // the far end of the /12
		"192.168.1.1",
		"100.64.0.1", // CGNAT
		"::1",
		"fe80::1",
		"fd00::1",
		"::ffff:127.0.0.1", // IPv4-mapped loopback
	} {
		if !IsBlockedIP(net.ParseIP(s)) {
			t.Errorf("%s is reachable", s)
		}
	}
	// Public addresses must still work, or webhooks are simply broken.
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:2800:220:1::1"} {
		if IsBlockedIP(net.ParseIP(s)) {
			t.Errorf("%s is blocked but should not be", s)
		}
	}
	// 172.32.x is outside the /12 and is public.
	if IsBlockedIP(net.ParseIP("172.32.0.1")) {
		t.Error("172.32.0.1 is outside RFC1918 and should be allowed")
	}
}

func TestValidateWebhookURL(t *testing.T) {
	for _, tc := range []struct {
		url     string
		wantErr bool
	}{
		{"https://hooks.example.com/x", false},
		{"http://example.com:8080/x", false},
		{"https://172.32.0.1/x", false},

		{"ftp://example.com/x", true},
		{"file:///etc/passwd", true},
		{"", true},
		{"https://", true},
		{"http://localhost:3000/x", true},
		{"http://LOCALHOST/x", true},
		{"http://foo.localhost/x", true},
		{"http://127.0.0.1/x", true},
		{"http://169.254.169.254/latest/meta-data/", true},
		{"http://[::1]:8080/x", true},
		{"http://192.168.1.5/x", true},
	} {
		err := ValidateWebhookURL(tc.url)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateWebhookURL(%q) = %v, wantErr=%v", tc.url, err, tc.wantErr)
		}
	}
}

// The binding guarantee: a hostname that passes ValidateWebhookURL but resolves to a
// blocked address must still fail, because that is what a URL-time check cannot catch.
func TestGuardedDialerRefusesResolvedPrivateAddresses(t *testing.T) {
	client := &http.Client{Transport: &http.Transport{DialContext: GuardedDialer()}}

	// A real loopback listener, addressed by a name that resolves to it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	// localhost resolves to 127.0.0.1 — the URL string alone says nothing.
	req, _ := http.NewRequest(http.MethodGet, "http://localhost:"+itoa(port)+"/", nil)
	if _, err := client.Do(req); err == nil {
		t.Fatal("connected to a name resolving to loopback")
	} else if !strings.Contains(err.Error(), ErrBlockedAddress.Error()) {
		t.Errorf("err = %v, want it to mention a blocked address", err)
	}
}

func TestWebhookClientRefusesRedirects(t *testing.T) {
	// A receiver that answers 302 — the standard way around an egress allowlist.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer target.Close()

	// Dial directly so the test exercises redirect policy rather than the IP guard.
	c := &http.Client{CheckRedirect: WebhookClient().CheckRedirect, Timeout: 5 * time.Second}
	resp, err := c.Get(target.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want the 302 surfaced rather than followed", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc == "" {
		t.Error("no Location header — the redirect appears to have been followed")
	}
}

// One slow host must not consume every delivery slot.
func TestHostGateIsPerHost(t *testing.T) {
	g := NewHostGate(2)
	ctx := context.Background()

	r1, err := g.Acquire(ctx, "slow.example")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := g.Acquire(ctx, "slow.example")
	if err != nil {
		t.Fatal(err)
	}

	// Third slot for the same host must block...
	blocked := make(chan struct{})
	go func() {
		short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		if _, err := g.Acquire(short, "slow.example"); errors.Is(err, context.DeadlineExceeded) {
			close(blocked)
		}
	}()
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("a third delivery to a saturated host was admitted")
	}

	// ...while a different host is unaffected, which is the whole point.
	done := make(chan struct{})
	go func() {
		defer close(done)
		if rel, err := g.Acquire(ctx, "fast.example"); err == nil {
			rel()
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a healthy host was starved by a saturated one")
	}

	r1()
	r2()
}

func TestHostGateReleasesSlots(t *testing.T) {
	g := NewHostGate(1)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rel, err := g.Acquire(ctx, "h")
			if err != nil {
				t.Error(err)
				return
			}
			rel()
		}()
	}
	waited := make(chan struct{})
	go func() { wg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("slots were not released — deliveries would deadlock")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
