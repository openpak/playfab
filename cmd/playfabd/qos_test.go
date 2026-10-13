package main

import (
	"bytes"
	"encoding/json"
	"net"
	"testing"
	"time"
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
