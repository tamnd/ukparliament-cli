package ukparliament_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/ukparliament-cli/ukparliament"
)

// newTestClient wires a Client to talk to the given members and bills test servers.
func newTestClient(membersURL, billsURL string) *ukparliament.Client {
	cfg := ukparliament.DefaultConfig()
	cfg.BaseURL = membersURL
	cfg.BillsURL = billsURL
	cfg.Rate = 0
	return ukparliament.NewClientFromConfig(cfg)
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := ukparliament.NewClient()
	c.Rate = 0

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := ukparliament.NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestSearchMembers(t *testing.T) {
	payload := map[string]any{
		"totalResults": 1,
		"items": []map[string]any{
			{
				"value": map[string]any{
					"id":            1172,
					"nameDisplayAs": "Parmjit Singh Gill",
					"latestParty":   map[string]any{"name": "Liberal Democrat"},
					"latestHouseMembership": map[string]any{
						"house": 1,
						"membershipStatus": map[string]any{
							"statusDescription": "Current Member",
						},
						"membershipFrom": map[string]any{"name": "Leicester East"},
					},
				},
			},
		},
	}

	membersSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/Members/Search") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer membersSrv.Close()

	c := newTestClient(membersSrv.URL, "http://127.0.0.1:1") // bills unused

	members, err := c.SearchMembers(context.Background(), "Parmjit", "", true, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 {
		t.Fatalf("got %d members, want 1", len(members))
	}
	m := members[0]
	if m.ID != 1172 {
		t.Errorf("ID = %d, want 1172", m.ID)
	}
	if m.Name != "Parmjit Singh Gill" {
		t.Errorf("Name = %q, want %q", m.Name, "Parmjit Singh Gill")
	}
	if m.Party != "Liberal Democrat" {
		t.Errorf("Party = %q, want Liberal Democrat", m.Party)
	}
	if m.House != "Commons" {
		t.Errorf("House = %q, want Commons", m.House)
	}
}

func TestGetMember(t *testing.T) {
	payload := map[string]any{
		"value": map[string]any{
			"id":            1172,
			"nameDisplayAs": "Parmjit Singh Gill",
			"latestParty":   map[string]any{"name": "Liberal Democrat"},
			"latestHouseMembership": map[string]any{
				"house": 1,
				"membershipStatus": map[string]any{
					"statusDescription": "Current Member",
				},
				"membershipFrom": map[string]any{"name": ""},
			},
		},
	}

	membersSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer membersSrv.Close()

	c := newTestClient(membersSrv.URL, "http://127.0.0.1:1")

	m, err := c.GetMember(context.Background(), 1172)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != 1172 {
		t.Errorf("ID = %d, want 1172", m.ID)
	}
	if m.Status != "Current Member" {
		t.Errorf("Status = %q, want Current Member", m.Status)
	}
}

func TestSearchBills(t *testing.T) {
	payload := map[string]any{
		"totalResults": 1,
		"items": []map[string]any{
			{
				"billId":       3973,
				"shortTitle":   "Climate and Nature Bill",
				"longTitle":    "A Bill to set targets for net-zero emissions and nature recovery.",
				"currentHouse": "Commons",
				"isAct":        false,
				"sponsors": []map[string]any{
					{"member": map[string]any{"name": "Caroline Lucas"}},
				},
			},
		},
	}

	billsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/Bills") {
			t.Errorf("unexpected bills path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer billsSrv.Close()

	c := newTestClient("http://127.0.0.1:1", billsSrv.URL)

	bills, err := c.SearchBills(context.Background(), "climate", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(bills) != 1 {
		t.Fatalf("got %d bills, want 1", len(bills))
	}
	b := bills[0]
	if b.ID != 3973 {
		t.Errorf("ID = %d, want 3973", b.ID)
	}
	if b.ShortTitle != "Climate and Nature Bill" {
		t.Errorf("ShortTitle = %q", b.ShortTitle)
	}
	if b.Sponsors != "Caroline Lucas" {
		t.Errorf("Sponsors = %q, want Caroline Lucas", b.Sponsors)
	}
}

func TestGetBill(t *testing.T) {
	payload := map[string]any{
		"billId":       3973,
		"shortTitle":   "Climate and Nature Bill",
		"longTitle":    "A Bill to set targets.",
		"currentHouse": "Commons",
		"isAct":        false,
		"sponsors":     []any{},
	}

	billsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer billsSrv.Close()

	c := newTestClient("http://127.0.0.1:1", billsSrv.URL)

	b, err := c.GetBill(context.Background(), 3973)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != 3973 {
		t.Errorf("ID = %d, want 3973", b.ID)
	}
	if b.CurrentHouse != "Commons" {
		t.Errorf("CurrentHouse = %q, want Commons", b.CurrentHouse)
	}
}
