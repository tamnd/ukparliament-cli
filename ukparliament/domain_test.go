package ukparliament

import (
	"testing"
)

// These tests are offline: they exercise the URI driver's pure string
// functions. The client's HTTP behaviour is covered in ukparliament_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "ukparliament" {
		t.Errorf("Scheme = %q, want ukparliament", info.Scheme)
	}
	if len(info.Hosts) == 0 {
		t.Error("Hosts is empty")
	}
	if info.Identity.Binary != "ukparliament" {
		t.Errorf("Identity.Binary = %q, want ukparliament", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
	}{
		{"1172", "memberid", "1172"},
		{"42", "memberid", "42"},
		{"johnson", "query", "johnson"},
		{"climate change", "query", "climate change"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestLocate(t *testing.T) {
	cases := []struct {
		uriType string
		id      string
		want    string
	}{
		{"memberid", "1172", "https://members.parliament.uk/member/1172/overview"},
		{"member", "1172", "https://members.parliament.uk/member/1172/overview"},
		{"query", "johnson", "https://members.parliament.uk/members/Commons"},
		{"bill", "3973", "https://bills.parliament.uk/bills/3973"},
	}
	for _, tc := range cases {
		got, err := Domain{}.Locate(tc.uriType, tc.id)
		if err != nil || got != tc.want {
			t.Errorf("Locate(%q, %q) = (%q, %v), want (%q, nil)",
				tc.uriType, tc.id, got, err, tc.want)
		}
	}
}
