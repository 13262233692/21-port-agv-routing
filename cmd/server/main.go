package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/port-agv/routing/internal/graph"
	"github.com/port-agv/routing/internal/grpc"
	"github.com/port-agv/routing/internal/mqtt"
)

type Config struct {
	GRPCPort int          `json:"grpc_port"`
	MQTT     mqtt.Config  `json:"mqtt"`
	Topology string       `json:"topology"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func buildDefaultTopology() *graph.Digraph {
	g := graph.NewDigraph()

	yardNodes := []struct {
		id       string
		x, y     float64
		deviceID string
	}{
		{"Y-01", 0, 0, "YC-01"},
		{"Y-02", 50, 0, "YC-02"},
		{"Y-03", 100, 0, "YC-03"},
		{"Y-04", 0, 50, "YC-04"},
		{"Y-05", 50, 50, "YC-05"},
		{"Y-06", 100, 50, "YC-06"},
	}
	for _, y := range yardNodes {
		g.AddNode(&graph.Node{
			ID: y.id, Type: graph.NodeYard,
			X: y.x, Y: y.y, DeviceID: y.deviceID,
		})
	}

	quayNodes := []struct {
		id       string
		x, y     float64
		deviceID string
	}{
		{"Q-01", 0, 300, "QC-01"},
		{"Q-02", 50, 300, "QC-02"},
		{"Q-03", 100, 300, "QC-03"},
	}
	for _, q := range quayNodes {
		g.AddNode(&graph.Node{
			ID: q.id, Type: graph.NodeQuayside,
			X: q.x, Y: q.y, DeviceID: q.deviceID,
		})
	}

	intersections := []struct {
		id   string
		x, y float64
	}{
		{"I-01", 0, 100},
		{"I-02", 50, 100},
		{"I-03", 100, 100},
		{"I-04", 0, 150},
		{"I-05", 50, 150},
		{"I-06", 100, 150},
		{"I-07", 0, 200},
		{"I-08", 50, 200},
		{"I-09", 100, 200},
		{"I-10", 0, 250},
		{"I-11", 50, 250},
		{"I-12", 100, 250},
	}
	for _, inter := range intersections {
		g.AddNode(&graph.Node{
			ID: inter.id, Type: graph.NodeIntersection,
			X: inter.x, Y: inter.y,
		})
	}

	g.AddNode(&graph.Node{ID: "B-01", Type: graph.NodeBufferZone, X: 25, Y: 200})
	g.AddNode(&graph.Node{ID: "B-02", Type: graph.NodeBufferZone, X: 75, Y: 200})

	g.AddNode(&graph.Node{ID: "C-01", Type: graph.NodeChargingStation, X: 50, Y: 125})

	type edgeDef struct {
		from, to   string
		weight     float64
		length     float64
		direction  float64
		maxSpeed   float64
	}
	edges := []edgeDef{
		{"Y-01", "I-01", 1.0, 100, 90, 6.0},
		{"Y-02", "I-02", 1.0, 100, 90, 6.0},
		{"Y-03", "I-03", 1.0, 100, 90, 6.0},
		{"Y-04", "I-01", 1.2, 100, 0, 4.0},
		{"Y-05", "I-02", 1.2, 100, 0, 4.0},
		{"Y-06", "I-03", 1.2, 100, 0, 4.0},

		{"I-01", "I-04", 1.0, 50, 90, 6.0},
		{"I-02", "I-05", 1.0, 50, 90, 6.0},
		{"I-03", "I-06", 1.0, 50, 90, 6.0},
		{"I-01", "I-02", 1.0, 50, 0, 6.0},
		{"I-02", "I-03", 1.0, 50, 0, 6.0},
		{"I-04", "I-05", 1.0, 50, 0, 6.0},
		{"I-05", "I-06", 1.0, 50, 0, 6.0},

		{"I-04", "I-07", 1.0, 50, 90, 6.0},
		{"I-05", "I-08", 1.0, 50, 90, 6.0},
		{"I-06", "I-09", 1.0, 50, 90, 6.0},
		{"I-07", "I-08", 1.0, 50, 0, 6.0},
		{"I-08", "I-09", 1.0, 50, 0, 6.0},

		{"I-07", "I-10", 1.0, 50, 90, 6.0},
		{"I-08", "I-11", 1.0, 50, 90, 6.0},
		{"I-09", "I-12", 1.0, 50, 90, 6.0},
		{"I-10", "I-11", 1.0, 50, 0, 6.0},
		{"I-11", "I-12", 1.0, 50, 0, 6.0},

		{"I-10", "Q-01", 1.0, 50, 90, 6.0},
		{"I-11", "Q-02", 1.0, 50, 90, 6.0},
		{"I-12", "Q-03", 1.0, 50, 90, 6.0},

		{"I-02", "I-01", 1.0, 50, 180, 6.0},
		{"I-03", "I-02", 1.0, 50, 180, 6.0},
		{"I-04", "I-01", 1.0, 50, 270, 6.0},
		{"I-05", "I-02", 1.0, 50, 270, 6.0},
		{"I-06", "I-03", 1.0, 50, 270, 6.0},
		{"I-07", "I-04", 1.0, 50, 270, 6.0},
		{"I-08", "I-05", 1.0, 50, 270, 6.0},
		{"I-09", "I-06", 1.0, 50, 270, 6.0},
		{"I-10", "I-07", 1.0, 50, 270, 6.0},
		{"I-11", "I-08", 1.0, 50, 270, 6.0},
		{"I-12", "I-09", 1.0, 50, 270, 6.0},
		{"Q-01", "I-10", 1.0, 50, 270, 6.0},
		{"Q-02", "I-11", 1.0, 50, 270, 6.0},
		{"Q-03", "I-12", 1.0, 50, 270, 6.0},

		{"I-05", "I-04", 1.0, 50, 180, 6.0},
		{"I-06", "I-05", 1.0, 50, 180, 6.0},
		{"I-08", "I-07", 1.0, 50, 180, 6.0},
		{"I-09", "I-08", 1.0, 50, 180, 6.0},
		{"I-11", "I-10", 1.0, 50, 180, 6.0},
		{"I-12", "I-11", 1.0, 50, 180, 6.0},

		{"I-08", "B-01", 1.5, 25, 270, 3.0},
		{"I-08", "B-02", 1.5, 25, 90, 3.0},
		{"B-01", "I-07", 1.5, 25, 270, 3.0},
		{"B-02", "I-09", 1.5, 25, 90, 3.0},

		{"I-05", "C-01", 1.3, 25, 270, 4.0},
		{"C-01", "I-05", 1.3, 25, 90, 4.0},
	}

	for _, e := range edges {
		g.AddEdge(&graph.Edge{
			From:       e.from,
			To:         e.to,
			BaseWeight: e.weight,
			Congestion: 0,
			IsBlocked:  false,
			MaxSpeed:   e.maxSpeed,
			Length:     e.length,
			Direction:  e.direction,
			IsActive:   true,
		})
	}

	return g
}

func main() {
	_ = flag.String("config", "", "path to config file")
	topologyPath := flag.String("topology", "", "path to topology JSON file")
	grpcPort := flag.Int("port", 50051, "gRPC server port")
	mqttBroker := flag.String("mqtt", "tcp://localhost:1883", "MQTT broker address")
	flag.Parse()

	log.Println("=== AGV Dispatch Center Starting ===")

	var g *graph.Digraph
	if *topologyPath != "" {
		var err error
		g, err = graph.LoadTopology(*topologyPath)
		if err != nil {
			log.Fatalf("Failed to load topology: %v", err)
		}
		log.Printf("Loaded topology from %s", *topologyPath)
	} else {
		g = buildDefaultTopology()
		log.Println("Using built-in demo topology")
	}
	log.Printf("Road network: %d nodes, %d edges", g.NodeCount(), g.EdgeCount())

	mqttConfig := mqtt.Config{
		Broker:   *mqttBroker,
		ClientID: "agv-dispatch-center",
		QoS:      1,
	}
	mqttClient := mqtt.NewClient(mqttConfig, g)

	if err := mqttClient.Connect(); err != nil {
		log.Printf("WARNING: MQTT connection failed: %v (continuing without MQTT)", err)
	} else {
		log.Println("MQTT client connected")
	}

	grpcServer := grpc.NewServer(g, mqttClient, *grpcPort)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := grpcServer.Start(); err != nil {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()

	sig := <-sigCh
	log.Printf("Received signal %v, shutting down...", sig)

	grpcServer.Stop()
	mqttClient.Disconnect()

	log.Println("AGV Dispatch Center stopped")
}
