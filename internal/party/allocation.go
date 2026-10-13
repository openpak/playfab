// Package party describes the native Party allocation contract inspected for
// Core Keeper. It does not implement an allocator or a relay.
package party

import (
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

type Request struct {
	BuildAliasParams *BuildAlias       `json:"BuildAliasParams,omitempty"`
	CustomTags       map[string]string `json:"CustomTags,omitempty"`
	PartyID          string            `json:"PartyId"`
	PreferredRegions []string          `json:"PreferredRegions,omitempty"`
	SessionCookie    string            `json:"SessionCookie,omitempty"`
	Version          string            `json:"Version,omitempty"`
}

type BuildAlias struct {
	AliasID string `json:"AliasId"`
}

// Allocation includes the connection fields validated by the native client.
// Other response metadata is deliberately not needed to select the relay.
type Allocation struct {
	PartyID                       string     `json:"PartyId"`
	Region                        string     `json:"Region"`
	FQDN                          string     `json:"FQDN"`
	IPV4Address                   string     `json:"IPV4Address,omitempty"`
	DTLSCertificateSHA2Thumbprint string     `json:"DTLSCertificateSHA2Thumbprint"`
	Ports                         []Port     `json:"Ports"`
	BuildID                       string     `json:"BuildId,omitempty"`
	ServerID                      string     `json:"ServerId,omitempty"`
	State                         string     `json:"State,omitempty"`
	VMID                          string     `json:"VmId,omitempty"`
	LastStateTransitionTime       *time.Time `json:"LastStateTransitionTime,omitempty"`
}

type Port struct {
	Name     string `json:"Name"`
	Num      int    `json:"Num"`
	Protocol string `json:"Protocol"`
}

type Connection struct {
	Region      string
	Host        string
	Port        uint16
	Fingerprint [32]byte
}

// Connection checks the allocation's client-visible connection constraints.
// A valid result is not evidence that a relay is running or accepting Party
// connections. The certificate fingerprint must match the actual DTLS relay.
func (a Allocation) Connection() (Connection, error) {
	var c Connection
	if !cString(a.Region, 20) || !cString(a.FQDN, 256) {
		return c, errors.New("invalid Party relay region or hostname")
	}
	// Emit canonical hexadecimal, rather than relying on the native parser's
	// optional separators. Require the SHA-256 fingerprint's exact length.
	if len(a.DTLSCertificateSHA2Thumbprint) != 64 {
		return c, errors.New("invalid Party relay certificate fingerprint")
	}
	fingerprint, err := hex.DecodeString(a.DTLSCertificateSHA2Thumbprint)
	if err != nil {
		return c, errors.New("invalid Party relay certificate fingerprint")
	}
	count := 0
	for _, p := range a.Ports {
		if p.Name != "XRNM" {
			continue
		}
		count++
		if count != 1 || p.Protocol != "UDP" || p.Num < 1 || p.Num > 65535 {
			return Connection{}, errors.New("invalid Party XRNM port")
		}
		c.Port = uint16(p.Num)
	}
	if count != 1 {
		return c, errors.New("missing Party XRNM port")
	}
	c.Region, c.Host = a.Region, a.FQDN
	copy(c.Fingerprint[:], fingerprint)
	return c, nil
}

func cString(s string, capacity int) bool {
	return s != "" && len(s) < capacity && !strings.ContainsRune(s, '\x00')
}
