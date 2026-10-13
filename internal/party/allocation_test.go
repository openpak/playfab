package party

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAllocationConnectionContract(t *testing.T) {
	var a Allocation
	err := json.Unmarshal([]byte(`{"Region":"UkSouth","FQDN":"relay.example.test","DTLSCertificateSHA2Thumbprint":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f","Ports":[{"Name":"metrics","Num":80,"Protocol":"TCP"},{"Name":"XRNM","Num":65535,"Protocol":"UDP"}]}`), &a)
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.Connection()
	if err != nil || c.Host != "relay.example.test" || c.Port != 65535 || c.Fingerprint[31] != 31 {
		t.Fatalf("connection=%+v error=%v", c, err)
	}

	cases := map[string]func(*Allocation){
		"missing region":    func(a *Allocation) { a.Region = "" },
		"long region":       func(a *Allocation) { a.Region = strings.Repeat("x", 20) },
		"region byte limit": func(a *Allocation) { a.Region = strings.Repeat("é", 10) },
		"embedded null":     func(a *Allocation) { a.FQDN = "relay\x00.example" },
		"long hostname":     func(a *Allocation) { a.FQDN = strings.Repeat("x", 256) },
		"short fingerprint": func(a *Allocation) { a.DTLSCertificateSHA2Thumbprint = "abcd" },
		"bad fingerprint":   func(a *Allocation) { a.DTLSCertificateSHA2Thumbprint = strings.Repeat("z", 64) },
		"missing port":      func(a *Allocation) { a.Ports = nil },
		"duplicate port":    func(a *Allocation) { a.Ports = append(a.Ports, a.Ports[1]) },
		"wrong protocol":    func(a *Allocation) { a.Ports[1].Protocol = "TCP" },
		"zero port":         func(a *Allocation) { a.Ports[1].Num = 0 },
		"negative port":     func(a *Allocation) { a.Ports[1].Num = -1 },
		"overflow port":     func(a *Allocation) { a.Ports[1].Num = 65536 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bad := a
			bad.Ports = append([]Port(nil), a.Ports...)
			mutate(&bad)
			if _, err := bad.Connection(); err == nil {
				t.Fatal("accepted incompatible relay allocation")
			}
		})
	}
}
