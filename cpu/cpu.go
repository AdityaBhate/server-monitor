package cpu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"servermonitor/websocket"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
)

// CPUStats holds CPU-related metrics
type CPUStats struct {
	// Overall CPU metrics
	TotalUsage float64
	UserSpace  float64
	System     float64
	Idle       float64
	IOWait     float64
	LoadAvg1   float64
	LoadAvg5   float64
	LoadAvg15  float64

	// Per-core metrics
	PerCoreUsage []float64

	// Thread count
	ThreadCount int32
}

type CPUMetricsJSON struct {
	Timestamp    string    `json:"timestamp"`
	TotalUsage   float64   `json:"total_usage"`
	UserSpace    float64   `json:"user_space"`
	System       float64   `json:"system"`
	Idle         float64   `json:"idle"`
	IOWait       float64   `json:"io_wait"`
	LoadAvg1     float64   `json:"load_avg_1"`
	LoadAvg5     float64   `json:"load_avg_5"`
	LoadAvg15    float64   `json:"load_avg_15"`
	PerCoreUsage []float64 `json:"per_core_usage"`
	ThreadCount  int32     `json:"thread_count"`
}

func broadcastCPUMetrics(stats *CPUStats) {
	metricsJSON := CPUMetricsJSON{
		Timestamp:    time.Now().Format(time.RFC3339),
		TotalUsage:   stats.TotalUsage,
		UserSpace:    stats.UserSpace,
		System:       stats.System,
		Idle:         stats.Idle,
		IOWait:       stats.IOWait,
		LoadAvg1:     stats.LoadAvg1,
		LoadAvg5:     stats.LoadAvg5,
		LoadAvg15:    stats.LoadAvg15,
		PerCoreUsage: stats.PerCoreUsage,
		ThreadCount:  stats.ThreadCount,
	}

	jsonData, err := json.Marshal(metricsJSON)
	if err != nil {
		log.Printf("Error marshaling CPU metrics to JSON: %v", err)
		return
	}

	websocket.BroadcastCPUMetrics(string(jsonData))
}

// CollectCPUMetrics starts CPU metrics collection
func CollectCPUMetrics(client influxdb2.Client) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	log.Println("Starting CPU metrics collection...")

	// Previous stats for rate calculations
	var prevStats *CPUStats

	for {
		select {
		case <-ticker.C:
			stats, err := gatherCPUStats()
			if err != nil {
				log.Printf("Error gathering CPU stats: %v", err)
				continue
			}

			logCPUMetrics(stats, prevStats)
			broadcastCPUMetrics(stats)

			err = sendCPUMetricsToInfluxDB(client, stats)
			if err != nil {
				log.Printf("Error sending CPU metrics to InfluxDB: %v", err)
			}

			prevStats = stats
		}
	}
}

// gatherCPUStats collects all CPU-related metrics
func gatherCPUStats() (*CPUStats, error) {
	stats := &CPUStats{}

	// Get overall CPU usage
	percentages, err := cpu.Percent(0, false)
	if err != nil {
		return nil, fmt.Errorf("error getting CPU percentage: %v", err)
	}
	if len(percentages) > 0 {
		stats.TotalUsage = percentages[0]
	}

	// Get per-core CPU usage
	perCore, err := cpu.Percent(0, true)
	if err != nil {
		return nil, fmt.Errorf("error getting per-core CPU percentage: %v", err)
	}
	stats.PerCoreUsage = perCore

	// Get CPU times to calculate user/system/idle percentages
	times, err := cpu.Times(false)
	if err != nil {
		return nil, fmt.Errorf("error getting CPU times: %v", err)
	}
	if len(times) > 0 {
		total := times[0].User + times[0].System + times[0].Idle + times[0].Iowait
		stats.UserSpace = (times[0].User / total) * 100
		stats.System = (times[0].System / total) * 100
		stats.Idle = (times[0].Idle / total) * 100
		stats.IOWait = (times[0].Iowait / total) * 100
	}

	// Get load averages
	loadAvg, err := load.Avg()
	if err != nil {
		return nil, fmt.Errorf("error getting load averages: %v", err)
	}
	stats.LoadAvg1 = loadAvg.Load1
	stats.LoadAvg5 = loadAvg.Load5
	stats.LoadAvg15 = loadAvg.Load15

	// Get thread count
	threads, err := cpu.Counts(false)
	if err != nil {
		return nil, fmt.Errorf("error getting CPU thread count: %v", err)
	}
	stats.ThreadCount = int32(threads)

	return stats, nil
}

// logCPUMetrics prints formatted CPU metrics
func logCPUMetrics(stats, prevStats *CPUStats) {
	log.Printf("\n=== CPU Statistics ===\n"+
		"Overall CPU Usage: %.2f%%\n"+
		"User Space: %.2f%%\n"+
		"System: %.2f%%\n"+
		"I/O Wait: %.2f%%\n"+
		"Idle: %.2f%%\n"+
		"Load Averages: %.2f, %.2f, %.2f (1, 5, 15 min)\n",
		stats.TotalUsage,
		stats.UserSpace,
		stats.System,
		stats.IOWait,
		stats.Idle,
		stats.LoadAvg1, stats.LoadAvg5, stats.LoadAvg15)

	log.Println("\nPer-Core Usage:")
	for i, usage := range stats.PerCoreUsage {
		log.Printf("Core %d: %.2f%%", i, usage)
	}

	log.Printf("\nTotal Threads Running: %d", stats.ThreadCount)
	log.Println("===========================")
}

// sendCPUMetricsToInfluxDB sends metrics to InfluxDB
func sendCPUMetricsToInfluxDB(client influxdb2.Client, stats *CPUStats) error {
	writeAPI := client.WriteAPIBlocking("my-org", "metrics")
	ctx := context.Background()
	timestamp := time.Now()

	// Write overall CPU metrics
	cpuPoint := influxdb2.NewPoint(
		"cpu_stats",
		map[string]string{
			"host": "server1",
			"type": "overall",
		},
		map[string]interface{}{
			"total_usage":  stats.TotalUsage,
			"user_space":   stats.UserSpace,
			"system":       stats.System,
			"idle":         stats.Idle,
			"iowait":       stats.IOWait,
			"load_avg_1":   stats.LoadAvg1,
			"load_avg_5":   stats.LoadAvg5,
			"load_avg_15":  stats.LoadAvg15,
			"thread_count": stats.ThreadCount,
		},
		timestamp,
	)

	if err := writeAPI.WritePoint(ctx, cpuPoint); err != nil {
		return fmt.Errorf("error writing CPU metrics: %v", err)
	}

	// Write per-core metrics
	for i, usage := range stats.PerCoreUsage {
		corePoint := influxdb2.NewPoint(
			"cpu_stats",
			map[string]string{
				"host": "server1",
				"type": "per_core",
				"core": fmt.Sprintf("core_%d", i),
			},
			map[string]interface{}{
				"usage": usage,
			},
			timestamp,
		)

		if err := writeAPI.WritePoint(ctx, corePoint); err != nil {
			log.Printf("Error writing core %d metrics: %v", i, err)
			continue
		}
	}

	return nil
}
