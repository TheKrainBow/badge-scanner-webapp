// Package intraclient ports mobile/app/.../api/IntraApi.kt: the 42 API v2
// client_credentials client used to resolve login/photo/coalitions and to
// post coalition scores / TIGs.
package intraclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	TokenURL string
	Endpoint string
	UID      string
	Secret   string
}

const (
	maxRetries          = 3
	defaultRetryAfterMs = 2000
)

type User struct {
	ID              string
	Login           string
	DisplayName     string
	ImageURL        string
	CursusIDs       []int
	Location        string
	Level           *float64
	CurrentProjects []string
}

type Coalition struct {
	ID       int
	Name     string
	Slug     string
	Color    string
	ImageURL string
	CoverURL string
	Score    int
}

type CoalitionsUser struct {
	ID          int
	CoalitionID int
}

type ClusterInfo struct {
	ID      int
	Name    string
	CDNLink string
}

type ClusterLocation struct {
	Host     string
	Login    string
	PhotoURL string
}

type Client struct {
	http *http.Client

	mu           sync.Mutex
	accessToken  string
	tokenExpires time.Time
}

func New() *Client {
	return &Client{http: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Client) getToken(cfg Config) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpires.Add(-60*time.Second)) {
		return c.accessToken, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", cfg.UID)
	form.Set("client_secret", cfg.Secret)
	form.Set("scope", "public projects profile elearning tig forum")

	req, err := http.NewRequest(http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.executeWithRetry(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("42 API token request failed: HTTP %d — check the API UID/secret", resp.StatusCode)
	}
	var root struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &root); err != nil || root.AccessToken == "" {
		return "", fmt.Errorf("could not parse 42 API token response")
	}
	if root.ExpiresIn == 0 {
		root.ExpiresIn = 3600
	}
	c.accessToken = root.AccessToken
	c.tokenExpires = time.Now().Add(time.Duration(root.ExpiresIn) * time.Second)
	return c.accessToken, nil
}

// executeWithRetry mirrors IntraApi.executeWithRetry: on 429, sleeps
// Retry-After (seconds) or defaultRetryAfterMs, retries up to maxRetries.
func (c *Client) executeWithRetry(req *http.Request) (*http.Response, error) {
	var bodyBytes []byte
	if req.Body != nil {
		bodyBytes, _ = io.ReadAll(req.Body)
		req.Body.Close()
	}
	attempt := 0
	for {
		attempt++
		if bodyBytes != nil {
			req.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt <= maxRetries {
			retryAfterMs := int64(defaultRetryAfterMs)
			if h := resp.Header.Get("Retry-After"); h != "" {
				if secs, err := strconv.ParseInt(h, 10, 64); err == nil {
					retryAfterMs = secs * 1000
				}
			}
			resp.Body.Close()
			time.Sleep(time.Duration(retryAfterMs) * time.Millisecond)
			continue
		}
		return resp, nil
	}
}

func (c *Client) authedRequest(cfg Config, method, rawURL string, form url.Values) (*http.Response, error) {
	token, err := c.getToken(cfg)
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c.executeWithRetry(req)
}

// FetchUser fetches by ft_id when available, by login otherwise.
func (c *Client) FetchUser(cfg Config, ftID, login string) (User, error) {
	key := strings.TrimSpace(ftID)
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(login))
	}
	if key == "" {
		return User{}, fmt.Errorf("no ft_id or ft_login to look up on the intranet")
	}
	resp, err := c.authedRequest(cfg, http.MethodGet, fmt.Sprintf("%s/users/%s", strings.TrimRight(cfg.Endpoint, "/"), key), nil)
	if err != nil {
		return User{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return User{}, fmt.Errorf("user '%s' not found on the intranet", key)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return User{}, fmt.Errorf("intranet request failed: HTTP %d", resp.StatusCode)
	}
	u, ok := parseUser(body)
	if !ok {
		return User{}, fmt.Errorf("could not parse intranet user response")
	}
	return u, nil
}

// FetchCoalitions treats 404 as "no coalitions_user record at all" (a
// genuine empty result, not a failure); any other non-2xx is a real error —
// same distinction IntraApi.kt draws.
func (c *Client) FetchCoalitions(cfg Config, login string) ([]Coalition, error) {
	resp, err := c.authedRequest(cfg, http.MethodGet,
		fmt.Sprintf("%s/users/%s/coalitions", strings.TrimRight(cfg.Endpoint, "/"), strings.ToLower(login)), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("coalitions request failed: HTTP %d", resp.StatusCode)
	}
	var raw []map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil
	}
	var out []Coalition
	for _, o := range raw {
		id, ok := toInt(o["id"])
		if !ok {
			continue
		}
		out = append(out, Coalition{
			ID:       id,
			Name:     toStr(o["name"]),
			Slug:     toStr(o["slug"]),
			Color:    toStr(o["color"]),
			ImageURL: toStr(o["image_url"]),
			CoverURL: toStr(o["cover_url"]),
			Score:    intOr0(toInt(o["score"])),
		})
	}
	return out, nil
}

// FetchCoalitionsUsers returns the login's coalitions_user records — their
// id (not the coalition id) is what POST /scores wants.
func (c *Client) FetchCoalitionsUsers(cfg Config, login string) ([]CoalitionsUser, error) {
	resp, err := c.authedRequest(cfg, http.MethodGet,
		fmt.Sprintf("%s/users/%s/coalitions_users", strings.TrimRight(cfg.Endpoint, "/"), strings.ToLower(login)), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("coalitions_users request failed: HTTP %d", resp.StatusCode)
	}
	var raw []map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil
	}
	var out []CoalitionsUser
	for _, o := range raw {
		id, ok1 := toInt(o["id"])
		coID, ok2 := toInt(o["coalition_id"])
		if !ok1 || !ok2 {
			continue
		}
		out = append(out, CoalitionsUser{ID: id, CoalitionID: coID})
	}
	return out, nil
}

// PostCoalitionScore is POST /coalitions/{id}/scores, form-encoded.
func (c *Client) PostCoalitionScore(cfg Config, coalitionID, coalitionsUserID, value int, reason string) error {
	form := url.Values{}
	form.Set("score[reason]", reason)
	form.Set("score[coalitions_user_id]", strconv.Itoa(coalitionsUserID))
	form.Set("score[value]", strconv.Itoa(value))
	resp, err := c.authedRequest(cfg, http.MethodPost,
		fmt.Sprintf("%s/coalitions/%d/scores", strings.TrimRight(cfg.Endpoint, "/"), coalitionID), form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("coalition score request failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

// ValidTigDuration mirrors the API's accepted TIG durations (2h/4h/8h, seconds).
func ValidTigDuration(seconds int) bool {
	return seconds == 7200 || seconds == 14400 || seconds == 28800
}

// PostTig is POST /closes — a "TIG" (serious_misconduct close with a
// community-service duration). durationSeconds must be 7200/14400/28800.
func (c *Client) PostTig(cfg Config, userID, closerID, reason string, durationSeconds int) error {
	if !ValidTigDuration(durationSeconds) {
		return fmt.Errorf("invalid TIG duration: %d", durationSeconds)
	}
	form := url.Values{}
	form.Set("close[user_id]", userID)
	form.Set("close[kind]", "serious_misconduct")
	form.Set("close[state]", "close")
	form.Set("close[closer_id]", closerID)
	form.Set("close[reason]", reason)
	form.Set("close[community_services_attributes][][duration]", strconv.Itoa(durationSeconds))
	resp, err := c.authedRequest(cfg, http.MethodPost, fmt.Sprintf("%s/closes", strings.TrimRight(cfg.Endpoint, "/")), form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("TIG request failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

// FetchClusters is GET /clusters?filter[campus_id]={campusId}.
func (c *Client) FetchClusters(cfg Config, campusID string) ([]ClusterInfo, error) {
	resp, err := c.authedRequest(cfg, http.MethodGet,
		fmt.Sprintf("%s/clusters?filter[campus_id]=%s", strings.TrimRight(cfg.Endpoint, "/"), url.QueryEscape(campusID)), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("clusters request failed: HTTP %d", resp.StatusCode)
	}
	var raw []map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil
	}
	var out []ClusterInfo
	for _, o := range raw {
		id, ok := toInt(o["id"])
		name := toStr(o["name"])
		cdn := toStr(o["cdn_link"])
		if !ok || name == "" || cdn == "" {
			continue
		}
		out = append(out, ClusterInfo{ID: id, Name: name, CDNLink: cdn})
	}
	return out, nil
}

// FetchClusterSvg fetches a cluster's seat-map SVG straight from the CDN —
// public, no bearer token needed.
func (c *Client) FetchClusterSvg(cdnLink string) (string, error) {
	resp, err := c.executeWithRetry(mustRequest(http.MethodGet, cdnLink))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("cluster SVG fetch failed: HTTP %d", resp.StatusCode)
	}
	return string(body), nil
}

// FetchActiveLocations pages through GET /campus/{id}/locations (capped at
// page[size]=100), 350ms between pages to stay under the 42 API's ~2 req/s limit.
func (c *Client) FetchActiveLocations(cfg Config, campusID string) ([]ClusterLocation, error) {
	var out []ClusterLocation
	page := 1
	for {
		resp, err := c.authedRequest(cfg, http.MethodGet, fmt.Sprintf(
			"%s/campus/%s/locations?sort=host&filter[active]=true&page[size]=100&page[number]=%d",
			strings.TrimRight(cfg.Endpoint, "/"), url.QueryEscape(campusID), page), nil)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("locations request failed: HTTP %d", resp.StatusCode)
		}
		var raw []map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			return out, nil
		}
		count := 0
		for _, o := range raw {
			host := toStr(o["host"])
			user, _ := o["user"].(map[string]any)
			if host == "" || user == nil {
				continue
			}
			login := toStr(user["login"])
			if login == "" {
				continue
			}
			photoURL := ""
			if image, ok := user["image"].(map[string]any); ok {
				photoURL = toStr(image["link"])
			}
			out = append(out, ClusterLocation{Host: host, Login: login, PhotoURL: photoURL})
			count++
		}
		if count < 100 {
			break
		}
		page++
		time.Sleep(350 * time.Millisecond)
	}
	return out, nil
}

func mustRequest(method, u string) *http.Request {
	req, _ := http.NewRequest(method, u, nil)
	return req
}

func parseUser(body []byte) (User, bool) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return User{}, false
	}
	imageURL := ""
	if image, ok := root["image"].(map[string]any); ok {
		if versions, ok := image["versions"].(map[string]any); ok {
			imageURL = toStr(versions["medium"])
		}
		if imageURL == "" {
			imageURL = toStr(image["link"])
		}
	}
	var cursusIDs []int
	var level *float64
	if cursusUsers, ok := root["cursus_users"].([]any); ok {
		for _, cuRaw := range cursusUsers {
			cu, ok := cuRaw.(map[string]any)
			if !ok {
				continue
			}
			cid, ok := toInt(cu["cursus_id"])
			if !ok {
				if cursus, ok := cu["cursus"].(map[string]any); ok {
					cid, ok = toInt(cursus["id"])
					if !ok {
						continue
					}
				} else {
					continue
				}
			}
			cursusIDs = appendUnique(cursusIDs, cid)
			if cid == 21 {
				if lv, ok := cu["level"].(float64); ok {
					level = &lv
				}
			}
		}
	}
	var currentProjects []string
	if projectsUsers, ok := root["projects_users"].([]any); ok {
		for _, puRaw := range projectsUsers {
			pu, ok := puRaw.(map[string]any)
			if !ok {
				continue
			}
			if toStr(pu["status"]) != "in_progress" {
				continue
			}
			if project, ok := pu["project"].(map[string]any); ok {
				if name := toStr(project["name"]); name != "" {
					currentProjects = append(currentProjects, name)
				}
			}
		}
	}
	return User{
		ID:              toStr(root["id"]),
		Login:           toStr(root["login"]),
		DisplayName:     toStr(root["displayname"]),
		ImageURL:        imageURL,
		CursusIDs:       cursusIDs,
		Location:        toStr(root["location"]),
		Level:           level,
		CurrentProjects: currentProjects,
	}, true
}

func appendUnique(list []int, v int) []int {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}

func toInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case string:
		n, err := strconv.Atoi(t)
		return n, err == nil
	default:
		return 0, false
	}
}

func intOr0(n int, ok bool) int {
	if !ok {
		return 0
	}
	return n
}
