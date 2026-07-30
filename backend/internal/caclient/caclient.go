// Package caclient ports mobile/app/.../api/CaApi.kt: the client for the
// access-control system ("CA", an Ixoff ibox). User pk is NOT the badge
// number — badge numbers live in each user's badges[].number, so badge
// resolution goes through the full user listing.
package caclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"badgescanner/backend/internal/store"
)

type Config struct {
	Endpoint string
	Username string
	Password string
}

type Client struct {
	http *http.Client
}

// New builds the CA HTTP client: 10s dial timeout, 120s response timeout
// (the CA is slow — same override as ScanViewModel's caApi client), plus
// the pinned-certificate TLS trust from CaTrust.kt.
func New() (*Client, error) {
	tlsCfg, err := pinnedTLSConfig()
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		TLSClientConfig:     tlsCfg,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &Client{http: &http.Client{Transport: transport, Timeout: 120 * time.Second}}, nil
}

type CAUser struct {
	FTLogin       string
	FTId          string
	AccessProfile int
}

// FetchAllUsers pages through GET /users/ (default page size 30), returning
// every entry — the caller filters to IsListable before persisting, exactly
// like CaDirectory.refresh does.
func (c *Client) FetchAllUsers(cfg Config, pageSize int, onPage func(fetched, total int)) ([]store.CADirEntry, error) {
	var out []store.CADirEntry
	next := fmt.Sprintf("%s/users/?limit=%d&page_size=%d", strings.TrimRight(cfg.Endpoint, "/"), pageSize, pageSize)
	visited := map[string]bool{}
	total := -1

	for next != "" {
		if visited[next] || len(visited) > 10_000 {
			break
		}
		visited[next] = true

		body, err := c.get(cfg, next)
		if err != nil {
			return nil, err
		}

		var root any
		if err := json.Unmarshal(body, &root); err != nil {
			return nil, fmt.Errorf("could not parse CA users response: %w", err)
		}

		var results []any
		var nextURL string
		switch v := root.(type) {
		case []any:
			results = v
		case map[string]any:
			if arr, ok := v["results"].([]any); ok {
				results = arr
			} else if arr, ok := v["data"].([]any); ok {
				results = arr
			}
			if cnt, ok := v["count"].(float64); ok {
				total = int(cnt)
			}
			if n, ok := v["next"].(string); ok {
				nextURL = n
			}
		}

		for _, r := range results {
			if e, ok := parseDirEntry(r); ok {
				out = append(out, e)
			}
		}
		if onPage != nil {
			onPage(len(out), total)
		}

		next = ""
		if nextURL != "" {
			next = rebase(nextURL, cfg.Endpoint)
		}
	}
	return out, nil
}

// FetchUser is GET /users/{pk}/ — the same fields watchdog's CreateNewUser reads.
func (c *Client) FetchUser(cfg Config, pk int64) (CAUser, error) {
	body, err := c.get(cfg, fmt.Sprintf("%s/users/%d/", strings.TrimRight(cfg.Endpoint, "/"), pk))
	if err != nil {
		return CAUser{}, err
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return CAUser{}, fmt.Errorf("could not parse CA user response: %w", err)
	}
	props, _ := root["properties"].(map[string]any)
	u := CAUser{}
	if props != nil {
		u.FTLogin, _ = props["ft_login"].(string)
		u.FTId, _ = props["ft_id"].(string)
	}
	if ap, ok := root["access_profile"]; ok {
		switch v := ap.(type) {
		case float64:
			u.AccessProfile = int(v)
		case string:
			n, _ := strconv.Atoi(v)
			u.AccessProfile = n
		}
	}
	return u, nil
}

func (c *Client) get(cfg Config, rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(cfg.Username, cfg.Password)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("CA authentication failed (%d) — check the CA username/password", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("CA request failed: HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func parseDirEntry(raw any) (store.CADirEntry, bool) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return store.CADirEntry{}, false
	}
	pk, ok := numericField(obj, "pk", "id")
	if !ok {
		return store.CADirEntry{}, false
	}
	e := store.CADirEntry{PK: int64(pk)}
	if fn, ok := obj["full_name"].(string); ok {
		e.FullName = fn
	}
	if props, ok := obj["properties"].(map[string]any); ok {
		if v, ok := props["ft_login"].(string); ok {
			e.FTLogin = v
		}
		if v, ok := props["ft_id"].(string); ok {
			e.FTId = v
		}
	}
	if badges, ok := obj["badges"].([]any); ok {
		for _, b := range badges {
			switch v := b.(type) {
			case map[string]any:
				if n, ok := numericField(v, "number"); ok {
					e.Badges = append(e.Badges, int64(n))
				}
			case float64:
				e.Badges = append(e.Badges, int64(v))
			case string:
				if n, err := strconv.ParseInt(v, 10, 64); err == nil {
					e.Badges = append(e.Badges, n)
				}
			}
		}
	}
	return e, true
}

func numericField(obj map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		switch v := obj[k].(type) {
		case float64:
			return v, true
		case string:
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

// rebase mirrors CaApi.rebase: `next` uses the ibox's internal hostname;
// keep only its path/query and rebase onto the configured endpoint.
func rebase(next, endpoint string) string {
	nextURL, err := url.Parse(next)
	if err != nil {
		return ""
	}
	base, err := url.Parse(endpoint)
	if err != nil {
		return next
	}
	nextURL.Scheme = base.Scheme
	nextURL.Host = base.Host
	return nextURL.String()
}
