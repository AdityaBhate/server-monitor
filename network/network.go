package network

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"servermonitor/websocket"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

// NetworkStats holds the network statistics
type NetworkStats struct {
	BytesIn       int64
	BytesOut      int64
	Packets       int64
	ProtocolStats map[string]int64            // Count by protocol
	PortStats     map[string]map[uint16]int64 // Count by direction and port
}

// Initialize new NetworkStats
func NewNetworkStats() NetworkStats {
	return NetworkStats{
		ProtocolStats: make(map[string]int64),
		PortStats: map[string]map[uint16]int64{
			"src": make(map[uint16]int64),
			"dst": make(map[uint16]int64),
		},
	}
}

var stats NetworkStats

// CollectNetworkMetrics monitors network traffic and sends data to InfluxDB
// CollectNetworkMetrics monitors network traffic and sends data to InfluxDB
func CollectNetworkMetrics(client influxdb2.Client) {
	// Initialize stats
	stats = NewNetworkStats()

	// Open the network interface for packet capture
	handle, err := pcap.OpenLive("enp0s3", 1600, true, pcap.BlockForever)
	if err != nil {
		log.Fatal("Error opening device: ", err)
	}
	defer handle.Close()

	// Set BPF filter for IP traffic
	err = handle.SetBPFFilter("ip")
	if err != nil {
		log.Fatal("Error setting BPF filter: ", err)
	}

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	packets := packetSource.Packets()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case packet := <-packets:
			if packet != nil {
				handlePacket(packet)
			}
		case <-ticker.C:
			// Send metrics to InfluxDB
			sendNetworkMetricsToInfluxDB(client)

			// Broadcast metrics to WebSocket clients
			sendNetworkMetrics()

			// Reset stats after sending
			stats = NewNetworkStats()
		}
	}
}

// getProtocolName returns the protocol name based on the protocol number
func getProtocolName(protocolNumber uint8) string {
	switch protocolNumber {
	case 1:
		return "ICMP"
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	default:
		return "OTHER"
	}
}

// handlePacket processes packets and extracts key metrics
func handlePacket(packet gopacket.Packet) {
	// Extract network layer (IP)
	networkLayer := packet.NetworkLayer()
	if networkLayer == nil {
		return
	}

	ipv4, ok := networkLayer.(*layers.IPv4)
	if !ok {
		return
	}

	// Update basic stats
	stats.Packets++

	// Get protocol name and update protocol stats
	protocolName := getProtocolName(uint8(ipv4.Protocol))
	stats.ProtocolStats[protocolName]++

	// Extract transport layer for port information
	transportLayer := packet.TransportLayer()
	var srcPort, dstPort uint16

	if transportLayer != nil {
		switch t := transportLayer.(type) {
		case *layers.TCP:
			srcPort = uint16(t.SrcPort)
			dstPort = uint16(t.DstPort)
		case *layers.UDP:
			srcPort = uint16(t.SrcPort)
			dstPort = uint16(t.DstPort)
		}
	}

	// Update port statistics
	if srcPort > 0 {
		stats.PortStats["src"][srcPort]++
	}
	if dstPort > 0 {
		stats.PortStats["dst"][dstPort]++
	}

	// Determine traffic direction and update byte counts
	localIP := "your.server.ip.address" // Replace with actual server IP
	if ipv4.SrcIP.String() == localIP {
		stats.BytesOut += int64(len(packet.Data()))
	} else if ipv4.DstIP.String() == localIP {
		stats.BytesIn += int64(len(packet.Data()))
	}

	log.Printf("Packet captured: %s:%d -> %s:%d Protocol: %s, Size: %d bytes",
		ipv4.SrcIP, srcPort,
		ipv4.DstIP, dstPort,
		protocolName,
		len(packet.Data()),
	)
}

// sendNetworkMetricsToInfluxDB sends the network metrics to InfluxDB
func sendNetworkMetricsToInfluxDB(client influxdb2.Client) {
	writeAPI := client.WriteAPIBlocking("my-org", "metrics")
	timestamp := time.Now()

	// Write basic network metrics
	basePoint := influxdb2.NewPoint(
		"network_traffic",
		map[string]string{
			"host":      "server1",
			"interface": "enp0s3",
		},
		map[string]interface{}{
			"bytes_in":  stats.BytesIn,
			"bytes_out": stats.BytesOut,
			"packets":   stats.Packets,
		},
		timestamp,
	)

	if err := writeAPI.WritePoint(context.Background(), basePoint); err != nil {
		log.Printf("Error writing base metrics to InfluxDB: %v", err)
		return
	}

	// Write protocol statistics
	for protocol, count := range stats.ProtocolStats {
		protocolPoint := influxdb2.NewPoint(
			"protocol_stats",
			map[string]string{
				"host":     "server1",
				"protocol": protocol,
			},
			map[string]interface{}{
				"count": count,
			},
			timestamp,
		)
		if err := writeAPI.WritePoint(context.Background(), protocolPoint); err != nil {
			log.Printf("Error writing protocol stats to InfluxDB: %v", err)
			continue
		}
	}

	// Write port statistics for top ports (top 10 most active)
	for direction, ports := range stats.PortStats {
		// Convert map to slice for sorting
		type portCount struct {
			port  uint16
			count int64
		}
		var portCounts []portCount
		for port, count := range ports {
			portCounts = append(portCounts, portCount{port, count})
		}

		// Sort by count in descending order
		sort.Slice(portCounts, func(i, j int) bool {
			return portCounts[i].count > portCounts[j].count
		})

		// Take top 10 ports
		for i := 0; i < len(portCounts) && i < 10; i++ {
			portPoint := influxdb2.NewPoint(
				"port_stats",
				map[string]string{
					"host":      "server1",
					"direction": direction,
					"port":      strconv.Itoa(int(portCounts[i].port)),
				},
				map[string]interface{}{
					"count": portCounts[i].count,
				},
				timestamp,
			)
			if err := writeAPI.WritePoint(context.Background(), portPoint); err != nil {
				log.Printf("Error writing port stats to InfluxDB: %v", err)
				continue
			}
		}
	}

	log.Printf("Network metrics written to InfluxDB - In: %d bytes, Out: %d bytes, Packets: %d",
		stats.BytesIn,
		stats.BytesOut,
		stats.Packets,
	)
}

func sendNetworkMetrics() {
	// Get top ports for a cleaner representation
	type portCount struct {
		port  uint16
		count int64
	}
	var topPorts []string
	for direction, ports := range stats.PortStats {
		var portCounts []portCount
		for port, count := range ports {
			portCounts = append(portCounts, portCount{port, count})
		}
		sort.Slice(portCounts, func(i, j int) bool {
			return portCounts[i].count > portCounts[j].count
		})
		// Take top 5 ports
		for i := 0; i < len(portCounts) && i < 5; i++ {
			topPorts = append(topPorts, fmt.Sprintf("%s:%d (%d)",
				direction,
				portCounts[i].port,
				portCounts[i].count))
		}
	}

	// Create a structured message matching client expectations
	networkMetrics := struct {
		Timestamp string           `json:"timestamp"`
		BytesIn   int64            `json:"BytesIn"`
		BytesOut  int64            `json:"BytesOut"`
		Packets   int64            `json:"Packets"`
		Protocols map[string]int64 `json:"Protocols"`
		PortStats string           `json:"PortStats"`
	}{
		Timestamp: time.Now().Format(time.RFC3339),
		BytesIn:   stats.BytesIn,
		BytesOut:  stats.BytesOut,
		Packets:   stats.Packets,
		Protocols: stats.ProtocolStats,
		PortStats: strings.Join(topPorts, ", "),
	}

	// Marshal the data to JSON
	networkMetricsJSON, err := json.Marshal(networkMetrics)
	if err != nil {
		log.Println("Error marshalling network metrics to JSON:", err)
		return
	}

	// Send the metrics to WebSocket clients
	websocket.BroadcastNetworkMetrics(string(networkMetricsJSON))

	log.Printf("Network metrics broadcasted to WebSocket clients")
}
