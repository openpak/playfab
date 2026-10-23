package party

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"

	"openpak/playfab/internal/routing"
)

// HomeRegion is the Azure name home's relay and beacon answer to.
const HomeRegion = "UkSouth"

// RelayService is the node role that runs a Party relay.
const RelayService = "playfab_party"

// maxPreferred bounds the work one request can ask for; the SDK sends a
// handful of QoS-ranked names.
const maxPreferred = 32

// Placement is where a new network's relay runs. Node "" is home.
type Placement struct {
	Region  string // Azure region name for the reply: always a table alias
	Node    string
	Address string // the node's relay address (host:port)
}

func (p Placement) IsHome() bool { return p.Node == "" }

func home() Placement { return Placement{Region: HomeRegion} }

// Place picks the relay for a new network (federation-worldwide-plan.md §1.1):
// the creator's PreferredRegions in order, the first whose own region has a
// relay node (or is home) wins; names the table does not know are skipped.
// With none, the account country decides, then home. A nil table is home.
func Place(t *routing.Table, preferred []string, acct routing.Account, networkID string) Placement {
	if t == nil {
		return home()
	}
	homeCode := t.Home
	if homeCode == "" {
		homeCode = routing.HomeRegion
	}
	if len(preferred) > maxPreferred {
		preferred = preferred[:maxPreferred]
	}
	for _, token := range preferred {
		regions := t.RegionsForToken(routing.StackPlayFab, token)
		if len(regions) == 0 {
			continue
		}
		res := t.Resolve(routing.Request{Stack: routing.StackPlayFab, Token: token, Country: acct.Country, Tester: acct.Tester, Service: RelayService, Key: networkID})
		if res.Via == routing.ViaExplicit {
			if p, ok := placement(t, res, token); ok {
				return p
			}
		}
		for _, r := range regions {
			if r == homeCode {
				return home() // the player chose home's region
			}
		}
		// The named region has no relay: a fallback would beat the client's
		// next-best measured region, so try that first.
	}
	res := t.Resolve(routing.Request{Stack: routing.StackPlayFab, Country: acct.Country, Tester: acct.Tester, Service: RelayService, Key: networkID})
	if p, ok := placement(t, res, ""); ok {
		return p
	}
	return home()
}

// placement names a node result with the table's spelling of token, or the
// region's canonical PlayFab name. A region with no usable name is not
// placed: only real Azure names are ever sent.
func placement(t *routing.Table, res routing.Result, token string) (Placement, bool) {
	if res.IsHome() || res.Address == "" {
		return Placement{}, false
	}
	token = routing.NormalizeToken(routing.StackPlayFab, token)
	for _, r := range t.Regions {
		if r.Code != res.Region {
			continue
		}
		names := r.Aliases[routing.StackPlayFab]
		name := ""
		for _, a := range names {
			if token != "" && strings.EqualFold(a, token) {
				name = a
				break
			}
		}
		if name == "" && len(names) > 0 {
			name = names[0]
		}
		if !cString(name, 20) {
			return Placement{}, false
		}
		return Placement{Region: name, Node: res.Node, Address: res.Address}, true
	}
	return Placement{}, false
}

// Allocation is the reply for this placement: home's allocation unchanged
// for home, otherwise home's with the node's region, relay host and XRNM
// port, and the node relay's DTLS certificate thumbprint.
func (p Placement) Allocation(homeAlloc Allocation, thumbprint string) (Allocation, error) {
	if p.IsHome() {
		return homeAlloc, nil
	}
	host, port, err := net.SplitHostPort(p.Address)
	n, perr := strconv.Atoi(port)
	if err != nil || perr != nil || host == "" {
		return Allocation{}, errors.New("invalid Party relay node address")
	}
	a := homeAlloc
	a.Region, a.FQDN, a.IPV4Address, a.DTLSCertificateSHA2Thumbprint = p.Region, host, "", thumbprint
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		a.IPV4Address = ip.To4().String()
	}
	a.Ports = nil
	for _, port := range homeAlloc.Ports {
		if port.Name == "XRNM" {
			port.Num = n
		}
		a.Ports = append(a.Ports, port)
	}
	if _, err := a.Connection(); err != nil {
		return Allocation{}, err
	}
	return a, nil
}

// Networks remembers where each network was placed. Only its creator's
// request places it; everyone who joins later gets the same relay, never
// their own preference.
type Networks struct {
	mu sync.Mutex
	m  map[string]Placement
}

// Place returns the network's placement, calling place only for a new one.
func (n *Networks) Place(networkID string, place func() Placement) Placement {
	n.mu.Lock()
	defer n.mu.Unlock()
	if p, ok := n.m[networkID]; ok {
		return p
	}
	if n.m == nil {
		n.m = map[string]Placement{}
	}
	p := place()
	n.m[networkID] = p
	return p
}

// Forget drops a network that has ended.
func (n *Networks) Forget(networkID string) {
	n.mu.Lock()
	delete(n.m, networkID)
	n.mu.Unlock()
}
