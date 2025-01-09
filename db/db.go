package db

import (
	"log"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

var influxClient influxdb2.Client

// initialize the InfluxDB client
func InitInfluxDB() influxdb2.Client {
	influxDBURL := "https://us-east-1-1.aws.cloud2.influxdata.com"                                              // InfluxDB instance URL
	influxDBToken := "4xgZGxX7Axx2V1mtu4c5uY0eEUdzBt7s-pVYtUL28LnEx-K4urDVwzB-IpXlQlIwxab7_tMbTcgM4SSHT-VlYA==" // Your InfluxDB token
	influxDBOrg := "ServerMonitor1"                                                                             // Your InfluxDB organization
	influxDBBucket := "metrics"                                                                                 // The bucket name to store metrics

	influxClient = influxdb2.NewClient(influxDBURL, influxDBToken)
	log.Println("Connected to InfluxDB")

	// Create the InfluxDB bucket if it doesn't exist
	err := createBucket(influxDBOrg, influxDBBucket)
	if err != nil {
		log.Fatalf("Failed to create bucket: %v", err)
	}

	return influxClient
}

// Create the InfluxDB bucket (if it doesn't exist)
func createBucket(org string, bucket string) error {
	// Placeholder for creating a bucket in InfluxDB
	// You can use InfluxDB's API to manage buckets and retention policies.
	return nil
}
