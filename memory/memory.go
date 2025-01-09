package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"servermonitor/websocket"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// MemoryStats holds all memory-related metrics
type MemoryStats struct {
	// RAM metrics
	TotalRAM      uint64
	UsedRAM       uint64
	FreeRAM       uint64
	RAMUsagePerc  float64
	BufferedRAM   uint64
	CachedRAM     uint64
	SwapTotal     uint64
	SwapUsed      uint64
	SwapFree      uint64
	SwapUsagePerc float64

	// Disk metrics
	DiskStats []DiskMetrics
}

// DiskMetrics holds per-disk statistics
type DiskMetrics struct {
	Device       string
	MountPoint   string
	TotalSize    uint64
	Used         uint64
	Free         uint64
	UsagePercent float64
	InodesFree   uint64
	InodesUsed   uint64
	InodesTotal  uint64
	ReadBytes    uint64
	WriteBytes   uint64
	ReadCount    uint64
	WriteCount   uint64
	ReadTime     uint64
	WriteTime    uint64
}

// FormatBytes converts bytes to human-readable format
func FormatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// calculateIORate calculates IO rate given bytes and time period
func calculateIORate(bytes uint64, duration time.Duration) float64 {
	return float64(bytes) / duration.Seconds()
}

var lastStats *MemoryStats
var lastCheckTime time.Time

// CollectMemoryMetrics starts the memory metrics collection
func CollectMemoryMetrics(client influxdb2.Client) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	log.Println("Starting memory metrics collection...")

	for {
		select {
		case <-ticker.C:
			stats, err := gatherMemoryStats()
			if err != nil {
				log.Printf("Error gathering memory stats: %v", err)
				continue
			}

			// Send metrics via WebSocket
			metricsJSON, err := json.Marshal(stats)
			if err != nil {
				log.Printf("Error marshaling memory stats: %v", err)
				continue
			}

			websocket.BroadcastMemoryMetrics(string(metricsJSON))

			// Optionally, send to InfluxDB
			err = sendMemoryMetricsToInfluxDB(client, stats)
			if err != nil {
				log.Printf("Error sending memory metrics to InfluxDB: %v", err)
			}
		}
	}
}

// logDetailedMetrics logs both memory and disk IO statistics
func logDetailedMetrics(currentStats, lastStats *MemoryStats, duration time.Duration) {
	// Log RAM and Swap usage
	log.Printf("\n=== Memory Statistics ===\n"+
		"RAM Usage: %.2f%% (Used: %s, Free: %s, Total: %s)\n"+
		"Swap Usage: %.2f%% (Used: %s, Free: %s, Total: %s)\n"+
		"Cached: %s, Buffered: %s\n",
		currentStats.RAMUsagePerc,
		FormatBytes(currentStats.UsedRAM),
		FormatBytes(currentStats.FreeRAM),
		FormatBytes(currentStats.TotalRAM),
		currentStats.SwapUsagePerc,
		FormatBytes(currentStats.SwapUsed),
		FormatBytes(currentStats.SwapFree),
		FormatBytes(currentStats.SwapTotal),
		FormatBytes(currentStats.CachedRAM),
		FormatBytes(currentStats.BufferedRAM))

	// Log Disk IO statistics for each disk
	log.Println("\n=== Disk I/O Statistics ===")
	for i, diskStat := range currentStats.DiskStats {
		lastDiskStat := lastStats.DiskStats[i]

		// Calculate IO rates
		readBytesRate := calculateIORate(diskStat.ReadBytes-lastDiskStat.ReadBytes, duration)
		writeBytesRate := calculateIORate(diskStat.WriteBytes-lastDiskStat.WriteBytes, duration)
		readOpsRate := calculateIORate(diskStat.ReadCount-lastDiskStat.ReadCount, duration)
		writeOpsRate := calculateIORate(diskStat.WriteCount-lastDiskStat.WriteCount, duration)

		log.Printf("Disk: %s (Mount: %s)\n"+
			"Usage: %.2f%% (Used: %s, Free: %s, Total: %s)\n"+
			"I/O Rates:\n"+
			"  Read: %.2f MB/s (%.2f ops/s)\n"+
			"  Write: %.2f MB/s (%.2f ops/s)\n",
			diskStat.Device,
			diskStat.MountPoint,
			diskStat.UsagePercent,
			FormatBytes(diskStat.Used),
			FormatBytes(diskStat.Free),
			FormatBytes(diskStat.TotalSize),
			readBytesRate/(1024*1024), // Convert to MB/s
			readOpsRate,
			writeBytesRate/(1024*1024), // Convert to MB/s
			writeOpsRate)
	}
	log.Println("===========================")
}

// gatherMemoryStats collects all memory-related metrics
func gatherMemoryStats() (*MemoryStats, error) {
	stats := &MemoryStats{}

	// Gather RAM metrics
	virtualMemory, err := mem.VirtualMemory()
	if err != nil {
		return nil, fmt.Errorf("error getting virtual memory stats: %v", err)
	}

	stats.TotalRAM = virtualMemory.Total
	stats.UsedRAM = virtualMemory.Used
	stats.FreeRAM = virtualMemory.Free
	stats.RAMUsagePerc = virtualMemory.UsedPercent
	stats.BufferedRAM = virtualMemory.Buffers
	stats.CachedRAM = virtualMemory.Cached

	// Gather swap metrics
	swapMemory, err := mem.SwapMemory()
	if err != nil {
		return nil, fmt.Errorf("error getting swap memory stats: %v", err)
	}

	stats.SwapTotal = swapMemory.Total
	stats.SwapUsed = swapMemory.Used
	stats.SwapFree = swapMemory.Free
	stats.SwapUsagePerc = swapMemory.UsedPercent

	// Gather disk metrics
	partitions, err := disk.Partitions(false)
	if err != nil {
		return nil, fmt.Errorf("error getting disk partitions: %v", err)
	}

	for _, partition := range partitions {
		diskMetric := DiskMetrics{
			Device:     partition.Device,
			MountPoint: partition.Mountpoint,
		}

		// Get usage statistics
		usage, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			log.Printf("Error getting disk usage for %s: %v", partition.Mountpoint, err)
			continue
		}

		diskMetric.TotalSize = usage.Total
		diskMetric.Used = usage.Used

		diskMetric.Free = usage.Free
		diskMetric.UsagePercent = usage.UsedPercent
		diskMetric.InodesFree = usage.InodesFree
		diskMetric.InodesUsed = usage.InodesUsed
		diskMetric.InodesTotal = usage.InodesTotal

		// Get I/O statistics
		ioStats, err := disk.IOCounters(partition.Device)
		if err != nil {
			log.Printf("Error getting disk IO stats for %s: %v", partition.Device, err)
		} else {
			if stat, ok := ioStats[partition.Device]; ok {
				diskMetric.ReadBytes = stat.ReadBytes
				diskMetric.WriteBytes = stat.WriteBytes
				diskMetric.ReadCount = stat.ReadCount
				diskMetric.WriteCount = stat.WriteCount
				diskMetric.ReadTime = stat.ReadTime
				diskMetric.WriteTime = stat.WriteTime
			}
		}

		stats.DiskStats = append(stats.DiskStats, diskMetric)
	}

	return stats, nil
}

// sendMemoryMetricsToInfluxDB sends the collected metrics to InfluxDB
func sendMemoryMetricsToInfluxDB(client influxdb2.Client, stats *MemoryStats) error {
	writeAPI := client.WriteAPIBlocking("my-org", "metrics")
	ctx := context.Background()
	timestamp := time.Now()

	// Write RAM metrics
	ramPoint := influxdb2.NewPoint(
		"memory_stats",
		map[string]string{
			"host": "server1",
			"type": "ram",
		},
		map[string]interface{}{
			"total":         stats.TotalRAM,
			"used":          stats.UsedRAM,
			"free":          stats.FreeRAM,
			"usage_percent": stats.RAMUsagePerc,
			"buffered":      stats.BufferedRAM,
			"cached":        stats.CachedRAM,
		},
		timestamp,
	)

	if err := writeAPI.WritePoint(ctx, ramPoint); err != nil {
		return fmt.Errorf("error writing RAM metrics: %v", err)
	}

	// Write swap metrics
	swapPoint := influxdb2.NewPoint(
		"memory_stats",
		map[string]string{
			"host": "server1",
			"type": "swap",
		},
		map[string]interface{}{
			"total":         stats.SwapTotal,
			"used":          stats.SwapUsed,
			"free":          stats.SwapFree,
			"usage_percent": stats.SwapUsagePerc,
		},
		timestamp,
	)

	if err := writeAPI.WritePoint(ctx, swapPoint); err != nil {
		return fmt.Errorf("error writing swap metrics: %v", err)
	}

	// Write disk metrics for each partition
	for _, diskStat := range stats.DiskStats {
		diskPoint := influxdb2.NewPoint(
			"disk_stats",
			map[string]string{
				"host":        "server1",
				"device":      diskStat.Device,
				"mount_point": diskStat.MountPoint,
			},
			map[string]interface{}{
				"total":         diskStat.TotalSize,
				"used":          diskStat.Used,
				"free":          diskStat.Free,
				"usage_percent": diskStat.UsagePercent,
				"inodes_free":   diskStat.InodesFree,
				"inodes_used":   diskStat.InodesUsed,
				"inodes_total":  diskStat.InodesTotal,
				"read_bytes":    diskStat.ReadBytes,
				"write_bytes":   diskStat.WriteBytes,
				"read_count":    diskStat.ReadCount,
				"write_count":   diskStat.WriteCount,
				"read_time":     diskStat.ReadTime,
				"write_time":    diskStat.WriteTime,
			},
			timestamp,
		)

		if err := writeAPI.WritePoint(ctx, diskPoint); err != nil {
			log.Printf("Error writing disk metrics for %s: %v", diskStat.Device, err)
			continue
		}
	}

	log.Printf("Memory metrics written to InfluxDB - RAM Usage: %.2f%%, Swap Usage: %.2f%%",
		stats.RAMUsagePerc,
		stats.SwapUsagePerc,
	)

	return nil
}
