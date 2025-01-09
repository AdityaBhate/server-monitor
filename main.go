package main

import (
	"log"
	"servermonitor/cpu"
	"servermonitor/db"
	"servermonitor/memory"
	"servermonitor/network"
	"servermonitor/websocket"
)

func main() {
	influxClient := db.InitInfluxDB()

	websocket.StartWebSocketServer()

	go network.CollectNetworkMetrics(influxClient)
	go cpu.CollectCPUMetrics(influxClient)
	go memory.CollectMemoryMetrics(influxClient)

	log.Println("Observer server is up and running...")

	// Keep the main goroutine alive
	select {}
}
