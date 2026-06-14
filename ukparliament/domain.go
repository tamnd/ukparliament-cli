package ukparliament

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes the UK Parliament data as a kit Domain. A multi-domain
// host (ant) enables it with a single blank import:
//
//	import _ "github.com/tamnd/ukparliament-cli/ukparliament"
//
// The same Domain also builds the standalone ukparliament binary (see
// cli.NewApp), so the binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the ukparliament driver. It carries no state; the per-run client
// is built by the factory Register hands to kit.
type Domain struct{}

// Info describes the scheme and the identity reused for the binary's help
// and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "ukparliament",
		Hosts:  []string{"members.parliament.uk", "bills.parliament.uk"},
		Identity: kit.Identity{
			Binary: "ukparliament",
			Short:  "A command line for the UK Parliament public APIs.",
			Long: `A command line for the UK Parliament public APIs.

ukparliament reads public data from the UK Parliament APIs over plain HTTPS,
shapes it into clean records, and prints output that pipes into the rest of
your tools. No API key, nothing to run alongside it.`,
			Site: "parliament.uk",
			Repo: "https://github.com/tamnd/ukparliament-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// members: search for MPs and Lords
	kit.Handle(app, kit.OpMeta{
		Name:    "members",
		Group:   "members",
		List:    true,
		Summary: "Search for members of Parliament",
		Args:    []kit.Arg{{Name: "name", Help: "name to search for (optional)"}},
	}, searchMembers)

	// member: get a single member by ID
	kit.Handle(app, kit.OpMeta{
		Name:     "member",
		Group:    "members",
		Single:   true,
		Summary:  "Get a single member of Parliament by ID",
		URIType:  "member",
		Resolver: true,
		Args:     []kit.Arg{{Name: "id", Help: "member numeric ID"}},
	}, getMember)

	// bills: search for bills
	kit.Handle(app, kit.OpMeta{
		Name:    "bills",
		Group:   "bills",
		List:    true,
		Summary: "Search for bills in Parliament",
	}, searchBills)

	// bill: get a single bill by ID
	kit.Handle(app, kit.OpMeta{
		Name:     "bill",
		Group:    "bills",
		Single:   true,
		Summary:  "Get a single bill by ID",
		URIType:  "bill",
		Resolver: true,
		Args:     []kit.Arg{{Name: "id", Help: "bill numeric ID"}},
	}, getBill)
}

// newClient builds the Client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
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
	return c, nil
}

// --- input structs ---

type membersInput struct {
	Name    string  `kit:"arg"  help:"name to search for"`
	House   string  `kit:"flag" help:"house: Commons or Lords"`
	Current bool    `kit:"flag" help:"current members only" default:"true"`
	Limit   int     `kit:"flag,inherit" help:"max results"`
	Client  *Client `kit:"inject"`
}

type memberInput struct {
	ID     string  `kit:"arg"    help:"member numeric ID"`
	Client *Client `kit:"inject"`
}

type billsInput struct {
	Query string  `kit:"flag" help:"search term"`
	House string  `kit:"flag" help:"house: Commons or Lords"`
	Limit int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type billInput struct {
	ID     string  `kit:"arg"    help:"bill numeric ID"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func searchMembers(ctx context.Context, in membersInput, emit func(*Member) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	members, err := in.Client.SearchMembers(ctx, in.Name, in.House, in.Current, limit)
	if err != nil {
		return mapErr(err)
	}
	for i := range members {
		if err := emit(&members[i]); err != nil {
			return err
		}
	}
	return nil
}

func getMember(ctx context.Context, in memberInput, emit func(*Member) error) error {
	id, err := parseID(in.ID)
	if err != nil {
		return errs.Usage("member id must be a number: %s", in.ID)
	}
	m, err := in.Client.GetMember(ctx, id)
	if err != nil {
		return mapErr(err)
	}
	return emit(m)
}

func searchBills(ctx context.Context, in billsInput, emit func(*Bill) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	bills, err := in.Client.SearchBills(ctx, in.Query, in.House, limit)
	if err != nil {
		return mapErr(err)
	}
	for i := range bills {
		if err := emit(&bills[i]); err != nil {
			return err
		}
	}
	return nil
}

func getBill(ctx context.Context, in billInput, emit func(*Bill) error) error {
	id, err := parseID(in.ID)
	if err != nil {
		return errs.Usage("bill id must be a number: %s", in.ID)
	}
	b, err := in.Client.GetBill(ctx, id)
	if err != nil {
		return mapErr(err)
	}
	return emit(b)
}

// --- Resolver: the URI-native string functions, pure and network-free ---

var digitRE = regexp.MustCompile(`^\d+$`)

// Classify turns any accepted input — a numeric member ID or a search term —
// into the canonical (type, id).
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if digitRE.MatchString(input) {
		return "memberid", input, nil
	}
	return "query", input, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "memberid":
		return "https://members.parliament.uk/member/" + id + "/overview", nil
	case "member":
		return "https://members.parliament.uk/member/" + id + "/overview", nil
	case "query":
		return "https://members.parliament.uk/members/Commons", nil
	case "bill":
		return "https://bills.parliament.uk/bills/" + id, nil
	default:
		return "", errs.Usage("ukparliament has no resource type %q", uriType)
	}
}

// --- helpers ---

func parseID(s string) (int, error) {
	s = strings.TrimSpace(s)
	var id int
	if _, err := fmt.Sscanf(s, "%d", &id); err != nil {
		return 0, err
	}
	return id, nil
}

func mapErr(err error) error {
	return err
}

