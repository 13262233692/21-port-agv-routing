package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/port-agv/routing/internal/graph"
)

type Config struct {
	Broker   string `json:"broker"`
	ClientID string `json:"client_id"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	QoS      byte   `json:"qos"`
}

type DeviceStatusMessage struct {
	DeviceID   string  `json:"device_id"`
	Type       string  `json:"type"`
	IsOnline   bool    `json:"is_online"`
	IsOccupied bool    `json:"is_occupied"`
	LoadFactor float64 `json:"load_factor"`
	Timestamp  int64   `json:"timestamp"`
}

type CongestionMessage struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	Congestion float64 `json:"congestion"`
	Timestamp  int64   `json:"timestamp"`
}

type AGVStatusMessage struct {
	AGVID     string  `json:"agv_id"`
	NodeID    string  `json:"node_id"`
	Speed     float64 `json:"speed"`
	Heading   float64 `json:"heading"`
	Battery   float64 `json:"battery"`
	Status    string  `json:"status"`
	Timestamp int64   `json:"timestamp"`
}

type Client struct {
	config             Config
	client             pahomqtt.Client
	graph              *graph.Digraph
	craneSensorHandler CraneSensorHandler
	mu                 sync.RWMutex
	stopCh             chan struct{}
}

type CraneSensorHandler func(payload []byte)

const (
	TopicDeviceStatus     = "port/device/+/status"
	TopicCongestion       = "port/congestion/+/+"
	TopicAGVStatus        = "port/agv/+/status"
	TopicControlFrame     = "port/agv/%s/command"
	TopicCraneSensor      = "port/crane/+/container_weight"
	TopicDevicePrefix     = "port/device/"
	TopicCongestionPrefix = "port/congestion/"
)

func NewClient(config Config, g *graph.Digraph) *Client {
	return &Client{
		config: config,
		graph:  g,
		stopCh: make(chan struct{}),
	}
}

func (c *Client) SetCraneSensorHandler(handler CraneSensorHandler) {
	c.craneSensorHandler = handler
}

func (c *Client) Connect() error {
	opts := pahomqtt.NewClientOptions()
	opts.AddBroker(c.config.Broker)
	opts.SetClientID(c.config.ClientID)
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(5 * time.Second)
	opts.SetKeepAlive(30 * time.Second)
	opts.SetPingTimeout(10 * time.Second)

	if c.config.Username != "" {
		opts.SetUsername(c.config.Username)
		opts.SetPassword(c.config.Password)
	}

	opts.SetOnConnectHandler(func(client pahomqtt.Client) {
		log.Printf("[MQTT] Connected to broker: %s", c.config.Broker)
		c.subscribeAll()
	})

	opts.SetConnectionLostHandler(func(client pahomqtt.Client, err error) {
		log.Printf("[MQTT] Connection lost: %v", err)
	})

	c.client = pahomqtt.NewClient(opts)
	if token := c.client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("mqtt connect failed: %w", token.Error())
	}

	return nil
}

func (c *Client) subscribeAll() {
	qos := c.config.QoS

	if token := c.client.Subscribe(TopicDeviceStatus, qos, c.handleDeviceStatus); token.Wait() && token.Error() != nil {
		log.Printf("[MQTT] Subscribe %s failed: %v", TopicDeviceStatus, token.Error())
	} else {
		log.Printf("[MQTT] Subscribed to %s", TopicDeviceStatus)
	}

	if token := c.client.Subscribe(TopicCongestion, qos, c.handleCongestion); token.Wait() && token.Error() != nil {
		log.Printf("[MQTT] Subscribe %s failed: %v", TopicCongestion, token.Error())
	} else {
		log.Printf("[MQTT] Subscribed to %s", TopicCongestion)
	}

	if token := c.client.Subscribe(TopicAGVStatus, qos, c.handleAGVStatus); token.Wait() && token.Error() != nil {
		log.Printf("[MQTT] Subscribe %s failed: %v", TopicAGVStatus, token.Error())
	} else {
		log.Printf("[MQTT] Subscribed to %s", TopicAGVStatus)
	}

	if c.craneSensorHandler != nil {
		if token := c.client.Subscribe(TopicCraneSensor, qos, c.handleCraneSensor); token.Wait() && token.Error() != nil {
			log.Printf("[MQTT] Subscribe %s failed: %v", TopicCraneSensor, token.Error())
		} else {
			log.Printf("[MQTT] Subscribed to %s", TopicCraneSensor)
		}
	}
}

func (c *Client) handleDeviceStatus(client pahomqtt.Client, msg pahomqtt.Message) {
	var payload DeviceStatusMessage
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		log.Printf("[MQTT] Invalid device status payload: %v", err)
		return
	}

	status := &graph.DeviceStatus{
		DeviceID:   payload.DeviceID,
		IsOnline:   payload.IsOnline,
		IsOccupied: payload.IsOccupied,
		LoadFactor: payload.LoadFactor,
	}
	c.graph.UpdateDeviceStatus(status)
	c.graph.ApplyDeviceImpact(payload.DeviceID)

	log.Printf("[MQTT] Device %s: online=%v occupied=%v load=%.2f",
		payload.DeviceID, payload.IsOnline, payload.IsOccupied, payload.LoadFactor)
}

func (c *Client) handleCongestion(client pahomqtt.Client, msg pahomqtt.Message) {
	var payload CongestionMessage
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		log.Printf("[MQTT] Invalid congestion payload: %v", err)
		return
	}

	c.graph.UpdateCongestion(payload.From, payload.To, payload.Congestion)

	log.Printf("[MQTT] Congestion %s->%s: %.2f", payload.From, payload.To, payload.Congestion)
}

func (c *Client) handleAGVStatus(client pahomqtt.Client, msg pahomqtt.Message) {
	var payload AGVStatusMessage
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		log.Printf("[MQTT] Invalid AGV status payload: %v", err)
		return
	}

	log.Printf("[MQTT] AGV %s: node=%s speed=%.1f battery=%.0f%% status=%s",
		payload.AGVID, payload.NodeID, payload.Speed, payload.Battery, payload.Status)
}

func (c *Client) handleCraneSensor(client pahomqtt.Client, msg pahomqtt.Message) {
	if c.craneSensorHandler != nil {
		c.craneSensorHandler(msg.Payload())
	}
}

func (c *Client) PublishControlFrames(agvID string, frames interface{}) error {
	topic := fmt.Sprintf(TopicControlFrame, agvID)
	payload, err := json.Marshal(frames)
	if err != nil {
		return fmt.Errorf("marshal control frames failed: %w", err)
	}

	if token := c.client.Publish(topic, c.config.QoS, false, payload); token.Wait() && token.Error() != nil {
		return fmt.Errorf("publish to %s failed: %w", topic, token.Error())
	}

	log.Printf("[MQTT] Published %d bytes to %s", len(payload), topic)
	return nil
}

func (c *Client) Disconnect() {
	close(c.stopCh)
	if c.client != nil && c.client.IsConnected() {
		c.client.Disconnect(250)
		log.Println("[MQTT] Disconnected")
	}
}

func (c *Client) IsConnected() bool {
	if c.client == nil {
		return false
	}
	return c.client.IsConnected()
}
