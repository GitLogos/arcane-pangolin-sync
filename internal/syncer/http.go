package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	apiKey  string
	auth    string
	http    *http.Client
}

func newArcaneClient(cfg Config) *Client {
	return &Client{
		baseURL: cfg.ArcaneURL,
		apiKey:  cfg.ArcaneAPIKey,
		auth:    "arcane",
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func newPangolinClient(cfg Config) *Client {
	base := cfg.PangolinURL
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return &Client{
		baseURL: base,
		apiKey:  cfg.PangolinAPIKey,
		auth:    "bearer",
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) put(ctx context.Context, path string, body any, out any) error {
	return c.do(ctx, http.MethodPut, path, body, out)
}

func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) delete(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s failed: status=%d body=%s", method, path, resp.StatusCode, string(data))
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s %s: %w: %s", method, path, err, string(data))
	}
	return nil
}

func (c *Client) newStreamRequest(ctx context.Context, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	c.setAuth(req)
	return req, nil
}

func (c *Client) setAuth(req *http.Request) {
	switch c.auth {
	case "arcane":
		req.Header.Set("X-Api-Key", c.apiKey)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

func escape(value string) string {
	return url.PathEscape(value)
}

func queryInt(path, key string, value int) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + url.QueryEscape(key) + "=" + strconv.Itoa(value)
}
