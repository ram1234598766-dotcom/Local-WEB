package nat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	UPnPSSDPAddr     = "239.255.255.250:1900"
	UPnPMSearchMsg   = "M-SEARCH * HTTP/1.1\r\nHost: 239.255.255.250:1900\r\nMan: \"ssdp:discover\"\r\nST: upnp:rootdevice\r\nMX: 3\r\n\r\n"
	UPnPDescPath     = "/description.xml"
	UPnPAddPortMap   = "AddPortMapping"
	UPnPDeletePortMap = "DeletePortMapping"
)

type HolePunchClient struct {
	localAddr net.Addr
	mu        sync.Mutex
}

func NewHolePunchClient() *HolePunchClient {
	return &HolePunchClient{}
}

func (c *HolePunchClient) Start(ctx context.Context, localPort int) (net.PacketConn, error) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", localPort))
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	c.localAddr = conn.LocalAddr()
	go c.readLoop(ctx, conn)
	return conn, nil
}

func (c *HolePunchClient) readLoop(ctx context.Context, conn net.PacketConn) {
	buf := make([]byte, 2048)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, _, err := conn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			continue
		}
	}
}

func (c *HolePunchClient) Send(conn net.PacketConn, peerAddr *net.UDPAddr, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := conn.WriteTo(payload, peerAddr)
	return err
}

type UPnPClient struct {
	mu        sync.Mutex
	ctrlURL   string
	svcType   string
	available bool
}

func NewUPnPClient() *UPnPClient {
	return &UPnPClient{}
}

func (c *UPnPClient) Discover(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp", UPnPSSDPAddr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	if _, err := conn.WriteTo([]byte(UPnPMSearchMsg), addr); err != nil {
		return err
	}

	buf := make([]byte, 2048)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			break
		}
		resp := string(buf[:n])
		if strings.Contains(resp, "upnp:rootdevice") {
			location := extractHeader(resp, "Location")
			if location != "" {
				c.mu.Lock()
				c.ctrlURL = location
				c.available = true
				c.mu.Unlock()
				return nil
			}
		}
	}
	return errors.New("no UPnP device found")
}

func (c *UPnPClient) AddPortMapping(externalPort int, internalPort int, protocol string, leaseDuration int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.available || c.ctrlURL == "" {
		return errors.New("UPnP not available")
	}
	body := fmt.Sprintf(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
<s:Body><u:%s xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
<NewRemoteHost></NewRemoteHost>
<NewExternalPort>%d</NewExternalPort>
<NewProtocol>%s</NewProtocol>
<NewInternalPort>%d</NewInternalPort>
<NewInternalClient></NewInternalClient>
<NewEnabled>1</NewEnabled>
<NewPortMappingDescription>LocalWEB</NewPortMappingDescription>
<NewLeaseDuration>%d</NewLeaseDuration>
</u:%s></s:Body></s:Envelope>`,
		UPnPAddPortMap, externalPort, protocol, internalPort, leaseDuration, UPnPAddPortMap)

	req, _ := http.NewRequest("POST", c.ctrlURL, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", fmt.Sprintf(`"urn:schemas-upnp-org:service:WANIPConnection:1#%s"`, UPnPAddPortMap))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (c *UPnPClient) DeletePortMapping(externalPort int, protocol string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.available || c.ctrlURL == "" {
		return errors.New("UPnP not available")
	}
	body := fmt.Sprintf(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
<s:Body><u:%s xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
<NewRemoteHost></NewRemoteHost>
<NewExternalPort>%d</NewExternalPort>
<NewProtocol>%s</NewProtocol>
</u:%s></s:Body></s:Envelope>`,
		UPnPDeletePortMap, externalPort, protocol, UPnPDeletePortMap)

	req, _ := http.NewRequest("POST", c.ctrlURL, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", fmt.Sprintf(`"urn:schemas-upnp-org:service:WANIPConnection:1#%s"`, UPnPDeletePortMap))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (c *UPnPClient) IsAvailable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.available
}

type RelayNode struct {
	ID       [32]byte
	Addr     string
	Capacity int
	Load     float64
	Score    float64
}

type CircuitRelay struct {
	mu         sync.Mutex
	nodes      map[[32]byte]*RelayNode
	circles    map[string]*Circuit
	allowRelay bool
}

type Circuit struct {
	ID      string
	Hops    [][32]byte
	Created time.Time
}

func NewCircuitRelay() *CircuitRelay {
	return &CircuitRelay{
		nodes:   make(map[[32]byte]*RelayNode),
		circles: make(map[string]*Circuit),
	}
}

func (r *CircuitRelay) RegisterNode(node *RelayNode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[node.ID] = node
}

func (r *CircuitRelay) SelectRelay(requiredHops int) ([]*RelayNode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var candidates []*RelayNode
	for _, n := range r.nodes {
		if n.Score > 0.5 && float64(n.Capacity) > n.Load {
			candidates = append(candidates, n)
		}
	}
	if len(candidates) < requiredHops {
		return nil, errors.New("insufficient relays")
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > requiredHops {
		candidates = candidates[:requiredHops]
	}
	return candidates, nil
}

func (r *CircuitRelay) EstablishCircuit(src, dst [32]byte, hops int) (*Circuit, error) {
	relays, err := r.SelectRelay(hops)
	if err != nil {
		return nil, err
	}
	ids := make([][32]byte, len(relays))
	for i, n := range relays {
		ids[i] = n.ID
	}
	circuit := &Circuit{
		ID:      fmt.Sprintf("%x-%x", src[:8], dst[:8]),
		Hops:    ids,
		Created: time.Now(),
	}
	r.mu.Lock()
	r.circles[circuit.ID] = circuit
	r.mu.Unlock()
	return circuit, nil
}

func (r *CircuitRelay) RelayPacket(circuitID string, payload []byte, hopIndex int) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	circuit, ok := r.circles[circuitID]
	if !ok {
		return nil, errors.New("circuit not found")
	}
	if hopIndex < 0 || hopIndex >= len(circuit.Hops) {
		return nil, errors.New("invalid hop index")
	}
	_ = payload
	return []byte(fmt.Sprintf("relayed via hop %d", hopIndex)), nil
}

type UPnPDesc struct {
	XMLName xml.Name `xml:"root"`
	Device  struct {
		ServiceList struct {
			Services []struct {
				ServiceType string `xml:"serviceType"`
				ControlURL  string `xml:"controlURL"`
			} `xml:"service"`
		} `xml:"serviceList"`
	} `xml:"device"`
}

func extractHeader(resp, key string) string {
	scanner := bufio.NewScanner(bytes.NewReader([]byte(resp)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.ToLower(line), strings.ToLower(key)+":") {
			return strings.TrimSpace(line[len(key)+1:])
		}
	}
	return ""
}

func (c *UPnPClient) fetchDescription() error {
	resp, err := http.Get(c.ctrlURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var desc UPnPDesc
	if err := xml.NewDecoder(resp.Body).Decode(&desc); err != nil {
		return err
	}
	for _, svc := range desc.Device.ServiceList.Services {
		if svc.ServiceType == "urn:schemas-upnp-org:service:WANIPConnection:1" {
			c.mu.Lock()
			c.svcType = svc.ServiceType
			c.mu.Unlock()
			return nil
		}
	}
	return errors.New("WANIPConnection service not found")
}
