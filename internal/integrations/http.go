package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sony/gobreaker"
)

// jsonClient is the shared HTTP machinery for outbound adapters.
type jsonClient struct {
	base    string
	http    *http.Client
	breaker *gobreaker.CircuitBreaker
}

func newJSONClient(base string, timeout time.Duration, name string) jsonClient {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return jsonClient{
		base:    base,
		http:    &http.Client{Timeout: timeout},
		breaker: newBreaker(name),
	}
}

// postJSON POSTs body to path, decoding the response into out (if non-nil). The call
// runs through the circuit breaker so a failing service stops being hammered.
func (c jsonClient) postJSON(ctx context.Context, path string, body, out any) error {
	_, err := c.breaker.Execute(func() (any, error) {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(buf))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("%s %s: status %d", http.MethodPost, path, resp.StatusCode)
		}
		if out != nil {
			return nil, json.NewDecoder(resp.Body).Decode(out)
		}
		return nil, nil
	})
	return err
}
