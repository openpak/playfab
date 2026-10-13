package main

// PlayFab's documented beacon protocol reflects a UDP datagram on port 3075,
// replacing its leading 0xffff with 0x0000. No session or credential is carried
// by this protocol. The API advertises a beacon only after its socket is bound.

import (
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

const partyQosRegion = "UkSouth"
const maxQosPacket = 512

type partyQos struct {
	conn   *net.UDPConn
	host   string
	served atomic.Uint64
}

func openPartyQos(addr, host string) (*partyQos, error) {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return nil, errors.New("PLAYFAB_QOS_HOST must be the beacon's reachable IPv4 address")
	}
	listen, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", listen)
	if err != nil {
		return nil, err
	}
	return &partyQos{conn: conn, host: ip.To4().String()}, nil
}

func (s *server) listPartyQosServers(w http.ResponseWriter, r *http.Request) {
	if s.qos == nil {
		unavailable(w)
		return
	}
	var request struct {
		CustomTags map[string]string
	}
	if !decodeBody(w, r, &request) {
		return
	}
	success(w, map[string]any{
		"PageSize":   1,
		"QosServers": []map[string]string{{"Region": partyQosRegion, "ServerUrl": s.qos.host}},
	})
}

type qosRate struct {
	second int64
	count  uint16
}

// Responses never exceed the request size. Bound both per-address traffic and
// rate-tracking memory; do not log probe payloads or source addresses.
func (q *partyQos) serve() error {
	buffer := make([]byte, maxQosPacket+1)
	rates := make(map[string]qosRate)
	var lastSweep int64
	for {
		n, peer, err := q.conn.ReadFromUDP(buffer)
		if err != nil {
			return err
		}
		if n < 2 || n > maxQosPacket || buffer[0] != 0xff || buffer[1] != 0xff {
			continue
		}
		second := time.Now().Unix()
		if second-lastSweep >= 60 {
			for ip, rate := range rates {
				if second-rate.second >= 60 {
					delete(rates, ip)
				}
			}
			lastSweep = second
		}
		ip := peer.IP.String()
		rate, exists := rates[ip]
		if !exists && len(rates) >= 4096 {
			continue
		}
		if rate.second != second {
			rate = qosRate{second: second}
		}
		if rate.count >= 64 {
			continue
		}
		rate.count++
		rates[ip] = rate
		buffer[0], buffer[1] = 0, 0
		if written, err := q.conn.WriteToUDP(buffer[:n], peer); err == nil && written == n {
			q.served.Add(1)
		}
	}
}
