// Package ukparliament is the library behind the ukparliament command line:
// the HTTP client, request shaping, and the typed data models for the UK
// Parliament public APIs.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public site throws under load.
package ukparliament

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config holds the tunable knobs for a Client.
type Config struct {
	BaseURL   string        // Members API base URL
	BillsURL  string        // Bills API base URL
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults for a polite public-API client.
func DefaultConfig() Config {
	return Config{
		BaseURL:   "https://members-api.parliament.uk",
		BillsURL:  "https://bills-api.parliament.uk",
		UserAgent: "ukparliament-cli/0.1 (github.com/tamnd/ukparliament-cli)",
		Rate:      100 * time.Millisecond,
		Retries:   3,
		Timeout:   15 * time.Second,
	}
}

// Client talks to the UK Parliament APIs over HTTP.
type Client struct {
	HTTP       *http.Client
	UserAgent  string
	Rate       time.Duration
	Retries    int
	membersURL string
	billsURL   string
	last       time.Time
}

// NewClient returns a Client with the default configuration.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:       &http.Client{Timeout: cfg.Timeout},
		UserAgent:  cfg.UserAgent,
		Rate:       cfg.Rate,
		Retries:    cfg.Retries,
		membersURL: cfg.BaseURL,
		billsURL:   cfg.BillsURL,
	}
}

// NewClientFromConfig builds a Client from an explicit Config.
func NewClientFromConfig(cfg Config) *Client {
	c := NewClient()
	if cfg.BaseURL != "" {
		c.membersURL = cfg.BaseURL
	}
	if cfg.BillsURL != "" {
		c.billsURL = cfg.BillsURL
	}
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c
}

// Get fetches url and returns the response body. It paces and retries
// according to the client's settings.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- Output types ---

// Member is a UK Parliament member (MP or Lord).
type Member struct {
	ID           int    `json:"id" kit:"id"`
	Name         string `json:"name"`
	Party        string `json:"party"`
	House        string `json:"house"`
	Status       string `json:"status"`
	Constituency string `json:"constituency"`
}

// Bill is a UK Parliament bill.
type Bill struct {
	ID           int    `json:"id" kit:"id"`
	ShortTitle   string `json:"short_title"`
	LongTitle    string `json:"long_title"`
	CurrentHouse string `json:"current_house"`
	IsAct        bool   `json:"is_act"`
	Sponsors     string `json:"sponsors"`
}

// --- Wire types: Members API ---

type wireMembersResponse struct {
	TotalResults int              `json:"totalResults"`
	Items        []wireMemberItem `json:"items"`
}

type wireMemberItem struct {
	Value wireMember `json:"value"`
}

type wireMember struct {
	ID   int    `json:"id"`
	Name string `json:"nameDisplayAs"`
	Party struct {
		Name string `json:"name"`
	} `json:"latestParty"`
	Membership struct {
		House  int `json:"house"`
		Status struct {
			Description string `json:"statusDescription"`
		} `json:"membershipStatus"`
		Constituency struct {
			Name string `json:"name"`
		} `json:"membershipFrom"`
	} `json:"latestHouseMembership"`
}

func houseFromInt(h int) string {
	switch h {
	case 1:
		return "Commons"
	case 2:
		return "Lords"
	default:
		return ""
	}
}

func (w wireMember) toMember() *Member {
	return &Member{
		ID:           w.ID,
		Name:         w.Name,
		Party:        w.Party.Name,
		House:        houseFromInt(w.Membership.House),
		Status:       w.Membership.Status.Description,
		Constituency: w.Membership.Constituency.Name,
	}
}

// --- Wire types: Bills API ---

type wireBillsResponse struct {
	TotalResults int        `json:"totalResults"`
	Items        []wireBill `json:"items"`
}

type wireBill struct {
	BillID       int    `json:"billId"`
	ShortTitle   string `json:"shortTitle"`
	LongTitle    string `json:"longTitle"`
	CurrentHouse string `json:"currentHouse"`
	IsAct        bool   `json:"isAct"`
	Sponsors     []struct {
		Member struct {
			Name string `json:"name"`
		} `json:"member"`
	} `json:"sponsors"`
}

func (w wireBill) toBill() *Bill {
	var names []string
	for _, s := range w.Sponsors {
		if s.Member.Name != "" {
			names = append(names, s.Member.Name)
		}
	}
	return &Bill{
		ID:           w.BillID,
		ShortTitle:   w.ShortTitle,
		LongTitle:    w.LongTitle,
		CurrentHouse: w.CurrentHouse,
		IsAct:        w.IsAct,
		Sponsors:     strings.Join(names, ", "),
	}
}

// --- Client methods ---

// SearchMembers searches for members by name, with optional house and current filter.
func (c *Client) SearchMembers(ctx context.Context, name, house string, current bool, limit int) ([]Member, error) {
	params := url.Values{}
	if name != "" {
		params.Set("Name", name)
	}
	if current {
		params.Set("IsCurrentMember", "true")
	}
	if house != "" {
		switch strings.ToLower(house) {
		case "commons":
			params.Set("House", "1")
		case "lords":
			params.Set("House", "2")
		}
	}
	if limit > 0 {
		params.Set("take", fmt.Sprintf("%d", limit))
	}
	params.Set("skip", "0")

	rawURL := c.membersURL + "/api/Members/Search?" + params.Encode()
	body, err := c.Get(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	var resp wireMembersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode members response: %w", err)
	}

	members := make([]Member, 0, len(resp.Items))
	for _, item := range resp.Items {
		members = append(members, *item.Value.toMember())
	}
	return members, nil
}

// GetMember fetches a single member by numeric ID.
func (c *Client) GetMember(ctx context.Context, id int) (*Member, error) {
	rawURL := fmt.Sprintf("%s/api/Members/%d", c.membersURL, id)
	body, err := c.Get(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	var item wireMemberItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("decode member response: %w", err)
	}
	return item.Value.toMember(), nil
}

// SearchBills searches for bills with optional full-text query and house filter.
func (c *Client) SearchBills(ctx context.Context, query, house string, limit int) ([]Bill, error) {
	params := url.Values{}
	if house != "" {
		switch strings.ToLower(house) {
		case "commons":
			params.Set("CurrentHouse", "Commons")
		case "lords":
			params.Set("CurrentHouse", "Lords")
		}
	}
	if query != "" {
		params.Set("SearchTerm", query)
	}
	if limit > 0 {
		params.Set("Take", fmt.Sprintf("%d", limit))
	}
	params.Set("Skip", "0")

	rawURL := c.billsURL + "/api/v1/Bills?" + params.Encode()
	body, err := c.Get(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	var resp wireBillsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode bills response: %w", err)
	}

	bills := make([]Bill, 0, len(resp.Items))
	for _, item := range resp.Items {
		bills = append(bills, *item.toBill())
	}
	return bills, nil
}

// GetBill fetches a single bill by numeric ID.
func (c *Client) GetBill(ctx context.Context, id int) (*Bill, error) {
	rawURL := fmt.Sprintf("%s/api/v1/Bills/%d", c.billsURL, id)
	body, err := c.Get(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	var bill wireBill
	if err := json.Unmarshal(body, &bill); err != nil {
		return nil, fmt.Errorf("decode bill response: %w", err)
	}
	return bill.toBill(), nil
}
