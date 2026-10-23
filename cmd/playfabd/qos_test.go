package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"openpak/playfab/internal/routing"
)

func testBeacon(t *testing.T) *partyQos {
	t.Helper()
	q, err := openPartyQos("127.0.0.1:0", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- q.serve() }()
	t.Cleanup(func() {
		q.conn.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("beacon did not stop after closing its socket")
		}
	})
	return q
}

func TestPartyQosWireProtocol(t *testing.T) {
	q := testBeacon(t)
	client, err := net.DialUDP("udp4", nil, q.conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	request := append([]byte{0xff, 0xff}, []byte("opaque-probe-nonce")...)
	if _, err := client.Write(request); err != nil {
		t.Fatal(err)
	}
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	reply := make([]byte, 1024)
	n, err := client.Read(reply)
	if err != nil {
		t.Fatal(err)
	}
	expected := append([]byte{0, 0}, request[2:]...)
	if !bytes.Equal(reply[:n], expected) {
		t.Fatalf("beacon did not preserve nonce and replace marker: %x", reply[:n])
	}
	for _, invalid := range [][]byte{{0xff}, {0, 0, 1}, bytes.Repeat([]byte{0xff}, maxQosPacket+1)} {
		if _, err := client.Write(invalid); err != nil {
			t.Fatal(err)
		}
		client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		if _, err := client.Read(reply); err == nil {
			t.Fatal("invalid or oversized probe was reflected")
		} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
			t.Fatal(err)
		}
	}
	if q.served.Load() != 1 {
		t.Fatalf("served %d probes, expected only the valid probe", q.served.Load())
	}
}

func TestPartyQosDiscovery(t *testing.T) {
	q := testBeacon(t)
	s := &server{qos: q, titles: defaultTitles()}
	w := call(s, "/MultiplayerServer/ListPartyQosServers", "{}", nil)
	if w.Code != 200 {
		t.Fatalf("discovery status %d: %s", w.Code, w.Body)
	}
	var response struct {
		Code int
		Data struct {
			PageSize   int
			QosServers []struct{ Region, ServerUrl string }
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || response.Data.PageSize != 1 || len(response.Data.QosServers) != 1 {
		t.Fatalf("invalid SDK discovery envelope: %s", w.Body)
	}
	beacon := response.Data.QosServers[0]
	if beacon.Region != "UkSouth" || beacon.ServerUrl != "127.0.0.1" {
		t.Fatalf("discovery does not identify the bound beacon: %+v", beacon)
	}
	if w := call(s, "/MultiplayerServer/ListPartyQosServers", "{", nil); w.Code != 400 {
		t.Fatalf("malformed discovery request status %d", w.Code)
	}
}

func TestPartyQosRequiresUsableAddress(t *testing.T) {
	for _, host := range []string{"", "0.0.0.0", "::1", "224.0.0.1", "https://example.com", "example.com"} {
		if q, err := openPartyQos("127.0.0.1:0", host); err == nil {
			q.conn.Close()
			t.Fatalf("accepted unusable beacon address %q", host)
		}
	}
}

// routingCore serves a fixture routing table (public relay + beacon in
// sa-east, test beacon in Japan) and marks BAAS user "aaaa" as a tester.
func routingCore(t *testing.T, tester bool, nodes string) *routing.Client {
	t.Helper()
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(routing.KeyHeader) != "routing-key" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case routing.PathTable:
			table := routing.NewTable(nil)
			if err := json.Unmarshal([]byte(nodes), &table.Nodes); err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(table)
		case routing.PathAccount:
			if r.URL.Query().Get("namespace") != "switch" || r.URL.Query().Get("subject") != "aaaa" {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(routing.Account{Country: "BR", Tester: tester})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(core.Close)
	c := &routing.Client{BaseURL: core.URL, Key: "routing-key"}
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}

const fixtureNodes = `[
 {"id":"br-gru-1","region":"sa-east","country":"BR","services":{"playfab_party":"198.51.100.10:3074","playfab_qos":"198.51.100.10:3075"}},
 {"id":"br-gru-2","region":"sa-east","country":"BR","services":{"playfab_party":"198.51.100.11:3074"}},
 {"id":"us-bad-1","region":"us-east","country":"US","services":{"playfab_qos":"198.51.100.12:9999"}},
 {"id":"jp-test-1","region":"ap-northeast","country":"JP","test":true,"services":{"playfab_party":"203.0.113.20:3074","playfab_qos":"203.0.113.20"}}]`

func qosList(t *testing.T, s *server, headers map[string]string) []struct{ Region, ServerUrl string } {
	t.Helper()
	w := call(s, "/MultiplayerServer/ListPartyQosServers", "{}", headers)
	var response struct {
		Data struct {
			PageSize   int
			QosServers []struct{ Region, ServerUrl string }
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Data.PageSize != len(response.Data.QosServers) {
		t.Fatalf("discovery %d: %s", w.Code, w.Body)
	}
	return response.Data.QosServers
}

func TestPartyQosRegions(t *testing.T) {
	s, _, _ := testService(t)
	s.qos = testBeacon(t)
	entity := map[string]string{"X-EntityToken": loggedIn(t, s).EntityToken.EntityToken}
	home := []struct{ Region, ServerUrl string }{{"UkSouth", "127.0.0.1"}}
	brazil := append(home, struct{ Region, ServerUrl string }{"BrazilSouth", "198.51.100.10"})
	japan := append(brazil[:2:2], struct{ Region, ServerUrl string }{"JapanEast", "203.0.113.20"})
	check := func(name string, got, want []struct{ Region, ServerUrl string }) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
	}

	check("federation off", qosList(t, s, entity), home)
	today := call(s, "/MultiplayerServer/ListPartyQosServers", "{}", entity).Body.String()
	for name, c := range map[string]*routing.Client{"no routing URL": {}, "zero nodes": routingCore(t, true, `[]`)} {
		s.routing = c
		if got := call(s, "/MultiplayerServer/ListPartyQosServers", "{}", entity).Body.String(); got != today {
			t.Fatalf("%s changed the reply:\n%s\nwant\n%s", name, got, today)
		}
	}

	s.routing = routingCore(t, false, fixtureNodes)
	check("public account", qosList(t, s, entity), brazil)
	s.routing = routingCore(t, true, fixtureNodes)
	check("tester", qosList(t, s, entity), japan)
	check("no entity token is not a tester", qosList(t, s, nil), brazil)
	check("invalid entity token is not a tester", qosList(t, s, map[string]string{"X-EntityToken": "opet1.x.y"}), brazil)
}
