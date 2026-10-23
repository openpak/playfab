package party

import (
	"strings"
	"testing"

	"openpak/playfab/internal/routing"
)

// fixture: a public relay in sa-east (BrazilSouth) and a test relay in Japan.
func fixture() *routing.Table {
	return routing.NewTable([]routing.Node{
		{ID: "br-gru-1", Region: "sa-east", Country: "BR", Services: map[string]string{"playfab_party": "198.51.100.10:3074", "playfab_qos": "198.51.100.10:3075"}},
		{ID: "jp-test-1", Region: "ap-northeast", Country: "JP", Test: true, Services: map[string]string{"playfab_party": "203.0.113.20:3074", "playfab_qos": "203.0.113.20"}},
	})
}

func TestPlace(t *testing.T) {
	brazil := Placement{Region: "BrazilSouth", Node: "br-gru-1", Address: "198.51.100.10:3074"}
	japan := Placement{Region: "JapanEast", Node: "jp-test-1", Address: "203.0.113.20:3074"}
	cases := []struct {
		name      string
		table     *routing.Table
		preferred []string
		acct      routing.Account
		want      Placement
	}{
		{"nil table is home", nil, []string{"BrazilSouth"}, routing.Account{Country: "BR"}, home()},
		{"zero nodes is home", routing.NewTable(nil), []string{"BrazilSouth", "JapanEast"}, routing.Account{Country: "BR", Tester: true}, home()},
		{"preferred region with a node", fixture(), []string{"BrazilSouth"}, routing.Account{}, brazil},
		{"table spelling is sent", fixture(), []string{"brazilsouth"}, routing.Account{}, brazil},
		{"explicit preference beats country", fixture(), []string{"BrazilSouth"}, routing.Account{Country: "JP", Tester: true}, brazil},
		{"tester's first preference wins", fixture(), []string{"JapanEast", "BrazilSouth"}, routing.Account{Country: "BR", Tester: true}, japan},
		{"test node hidden from others", fixture(), []string{"JapanEast", "BrazilSouth"}, routing.Account{}, brazil},
		{"test node hidden, nothing else", fixture(), []string{"JapanWest"}, routing.Account{Country: "JP"}, home()},
		{"unknown names skipped", fixture(), []string{"nx", "Mars", "", "BrazilSouth"}, routing.Account{}, brazil},
		{"region without node falls to next preference", fixture(), []string{"EastUs", "BrazilSouth"}, routing.Account{}, brazil},
		{"home preference is home", fixture(), []string{"UkSouth", "BrazilSouth"}, routing.Account{Country: "BR"}, home()},
		{"no preference uses country", fixture(), nil, routing.Account{Country: "AR"}, brazil},
		{"unknown names then country", fixture(), []string{"Mars"}, routing.Account{Country: "BR"}, brazil},
		{"tester country", fixture(), nil, routing.Account{Country: "KR", Tester: true}, Placement{Region: "JapanEast", Node: "jp-test-1", Address: "203.0.113.20:3074"}},
		{"country without node is home", fixture(), []string{"EastUs"}, routing.Account{Country: "US"}, home()},
		{"nothing to go on is home", fixture(), nil, routing.Account{}, home()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Place(c.table, c.preferred, c.acct, "net-1"); got != c.want {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestPlacementAllocation(t *testing.T) {
	homeAlloc := Allocation{Region: "UkSouth", FQDN: "relay.home.test", DTLSCertificateSHA2Thumbprint: strings.Repeat("ab", 32),
		Ports: []Port{{Name: "XRNM", Num: 30000, Protocol: "UDP"}}}
	got, err := home().Allocation(homeAlloc, strings.Repeat("cd", 32))
	if err != nil || got.Region != "UkSouth" || got.FQDN != "relay.home.test" || got.DTLSCertificateSHA2Thumbprint != homeAlloc.DTLSCertificateSHA2Thumbprint || got.Ports[0].Num != 30000 {
		t.Fatalf("home allocation changed: %+v %v", got, err)
	}
	p := Place(fixture(), []string{"BrazilSouth"}, routing.Account{}, "net-1")
	got, err = p.Allocation(homeAlloc, strings.Repeat("cd", 32))
	if err != nil {
		t.Fatal(err)
	}
	c, err := got.Connection()
	if err != nil || c.Region != "BrazilSouth" || c.Host != "198.51.100.10" || c.Port != 3074 || got.IPV4Address != "198.51.100.10" || homeAlloc.Ports[0].Num != 30000 {
		t.Fatalf("node allocation %+v %+v %v", got, c, err)
	}
	if _, err := (Placement{Region: "BrazilSouth", Node: "x", Address: "no-port"}).Allocation(homeAlloc, strings.Repeat("cd", 32)); err == nil {
		t.Fatal("accepted a node address without a port")
	}
}

func TestJoinersGetTheNetworksRelay(t *testing.T) {
	var n Networks
	creator := n.Place("net-1", func() Placement { return Place(fixture(), []string{"BrazilSouth"}, routing.Account{}, "net-1") })
	joiner := n.Place("net-1", func() Placement {
		t.Fatal("a joiner placed the network again")
		return Placement{}
	})
	if joiner != creator || creator.Node != "br-gru-1" {
		t.Fatalf("joiner %+v, creator %+v", joiner, creator)
	}
	n.Forget("net-1")
	if p := n.Place("net-1", home); !p.IsHome() {
		t.Fatalf("forgotten network kept its placement: %+v", p)
	}
}
