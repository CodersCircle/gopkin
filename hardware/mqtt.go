package hardware

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// MQTT v3.1.1 control packet types.
const (
	mqttConnect    byte = 0x10
	mqttConnAck    byte = 0x20
	mqttPublish    byte = 0x30
	mqttSubscribe  byte = 0x82
	mqttSubAck     byte = 0x90
	mqttPingReq    byte = 0xC0
	mqttPingResp   byte = 0xD0
	mqttDisconnect byte = 0xE0
)

// MQTTMessageHandler is invoked whenever a matching PUBLISH message arrives.
type MQTTMessageHandler func(topic string, payload []byte)

type mqttSubscription struct {
	filter  string
	handler MQTTMessageHandler
}

// MQTTClient is a lightweight, 100% Pure-Go MQTT v3.1.1 TCP client built solely
// on the Go standard library (zero external dependencies, zero CGO).
type MQTTClient struct {
	writeMu sync.Mutex
	conn    net.Conn
	br      *bufio.Reader
	bw      *bufio.Writer

	stateMu   sync.Mutex
	connected bool
	nextPktID uint16

	subMu sync.RWMutex
	subs  []mqttSubscription

	ackMu       sync.Mutex
	subAckChans map[uint16]chan error
	pingCh      chan struct{}

	doneCh   chan struct{}
	timeout  time.Duration
}

// NewMQTTClient creates an unconnected MQTT v3.1.1 client.
func NewMQTTClient() *MQTTClient {
	return &MQTTClient{
		subAckChans: make(map[uint16]chan error),
		pingCh:      make(chan struct{}, 1),
		timeout:     5 * time.Second,
	}
}

// Connect establishes a TCP connection to brokerAddr and performs the MQTT v3.1.1
// CONNECT / CONNACK handshake.
func (c *MQTTClient) Connect(brokerAddr, clientID, username, password string) error {
	if strings.TrimSpace(brokerAddr) == "" {
		return errors.New("hardware/mqtt: broker address cannot be empty")
	}
	if strings.TrimSpace(clientID) == "" {
		clientID = fmt.Sprintf("gopkin-%d", time.Now().UnixNano())
	}

	conn, err := net.DialTimeout("tcp", brokerAddr, c.timeout)
	if err != nil {
		return fmt.Errorf("hardware/mqtt: dial %s: %w", brokerAddr, err)
	}

	br := bufio.NewReaderSize(conn, 16*1024)
	bw := bufio.NewWriterSize(conn, 16*1024)

	// Build MQTT v3.1.1 CONNECT variable header + payload.
	var body bytes.Buffer
	writeMQTTString(&body, "MQTT")
	body.WriteByte(0x04) // Protocol Level 4 (MQTT 3.1.1)

	var flags byte = 0x02 // Clean Session
	if username != "" {
		flags |= 0x80
		if password != "" {
			flags |= 0x40
		}
	}
	body.WriteByte(flags)
	_ = binary.Write(&body, binary.BigEndian, uint16(60)) // KeepAlive 60s

	writeMQTTString(&body, clientID)
	if username != "" {
		writeMQTTString(&body, username)
		if password != "" {
			writeMQTTString(&body, password)
		}
	}

	if err := writeMQTTPacket(bw, mqttConnect, body.Bytes()); err != nil {
		_ = conn.Close()
		return fmt.Errorf("hardware/mqtt: send CONNECT: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(c.timeout))
	pktType, payload, err := readMQTTPacket(br)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("hardware/mqtt: read CONNACK: %w", err)
	}
	if pktType&0xF0 != mqttConnAck || len(payload) < 2 {
		_ = conn.Close()
		return fmt.Errorf("hardware/mqtt: expected CONNACK, got packet 0x%02x", pktType)
	}
	if payload[1] != 0x00 {
		_ = conn.Close()
		return fmt.Errorf("hardware/mqtt: connection refused (code %d)", payload[1])
	}

	c.writeMu.Lock()
	c.conn = conn
	c.br = br
	c.bw = bw
	c.writeMu.Unlock()

	c.stateMu.Lock()
	c.connected = true
	c.doneCh = make(chan struct{})
	doneCh := c.doneCh
	c.stateMu.Unlock()

	go c.readLoop(br, doneCh)
	return nil
}

// Publish sends a QoS 0 PUBLISH packet to topic with payload.
func (c *MQTTClient) Publish(topic string, payload []byte) error {
	if strings.TrimSpace(topic) == "" {
		return errors.New("hardware/mqtt: topic cannot be empty")
	}
	if !c.IsConnected() {
		return errors.New("hardware/mqtt: not connected")
	}

	var body bytes.Buffer
	writeMQTTString(&body, topic)
	body.Write(payload)

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.bw == nil {
		return errors.New("hardware/mqtt: not connected")
	}
	return writeMQTTPacket(c.bw, mqttPublish, body.Bytes())
}

// Subscribe registers handler for topic (supporting + and # wildcards) and sends
// a SUBSCRIBE packet, waiting for the broker's SUBACK.
func (c *MQTTClient) Subscribe(topic string, handler MQTTMessageHandler) error {
	if strings.TrimSpace(topic) == "" {
		return errors.New("hardware/mqtt: subscribe topic cannot be empty")
	}
	if !c.IsConnected() {
		return errors.New("hardware/mqtt: not connected")
	}

	c.subMu.Lock()
	c.subs = append(c.subs, mqttSubscription{filter: topic, handler: handler})
	c.subMu.Unlock()

	c.stateMu.Lock()
	c.nextPktID++
	if c.nextPktID == 0 {
		c.nextPktID = 1
	}
	pktID := c.nextPktID
	c.stateMu.Unlock()

	ackCh := make(chan error, 1)
	c.ackMu.Lock()
	c.subAckChans[pktID] = ackCh
	c.ackMu.Unlock()
	defer func() {
		c.ackMu.Lock()
		delete(c.subAckChans, pktID)
		c.ackMu.Unlock()
	}()

	var body bytes.Buffer
	_ = binary.Write(&body, binary.BigEndian, pktID)
	writeMQTTString(&body, topic)
	body.WriteByte(0x00) // Requested QoS 0

	c.writeMu.Lock()
	err := writeMQTTPacket(c.bw, mqttSubscribe, body.Bytes())
	c.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("hardware/mqtt: send SUBSCRIBE: %w", err)
	}

	select {
	case err := <-ackCh:
		return err
	case <-time.After(c.timeout):
		return errors.New("hardware/mqtt: timed out waiting for SUBACK")
	}
}

// Ping sends PINGREQ and waits for PINGRESP from the broker.
func (c *MQTTClient) Ping() error {
	if !c.IsConnected() {
		return errors.New("hardware/mqtt: not connected")
	}

	// Drain any stale ping token.
	select {
	case <-c.pingCh:
	default:
	}

	c.writeMu.Lock()
	err := writeMQTTPacket(c.bw, mqttPingReq, nil)
	c.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("hardware/mqtt: send PINGREQ: %w", err)
	}

	select {
	case <-c.pingCh:
		return nil
	case <-time.After(c.timeout):
		return errors.New("hardware/mqtt: timed out waiting for PINGRESP")
	}
}

// Disconnect sends a DISCONNECT packet and closes the underlying TCP connection.
func (c *MQTTClient) Disconnect() error {
	c.stateMu.Lock()
	wasConnected := c.connected
	c.connected = false
	doneCh := c.doneCh
	c.stateMu.Unlock()

	if !wasConnected {
		return nil
	}

	c.writeMu.Lock()
	if c.bw != nil {
		_ = writeMQTTPacket(c.bw, mqttDisconnect, nil)
	}
	var closeErr error
	if c.conn != nil {
		closeErr = c.conn.Close()
		c.conn = nil
		c.bw = nil
		c.br = nil
	}
	c.writeMu.Unlock()

	if doneCh != nil {
		<-doneCh
	}
	return closeErr
}

// IsConnected reports whether the client currently holds an active broker session.
func (c *MQTTClient) IsConnected() bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.connected
}

func (c *MQTTClient) readLoop(br *bufio.Reader, doneCh chan struct{}) {
	defer close(doneCh)
	for {
		pktType, payload, err := readMQTTPacket(br)
		if err != nil {
			c.stateMu.Lock()
			c.connected = false
			c.stateMu.Unlock()
			return
		}

		switch pktType & 0xF0 {
		case mqttPublish:
			qos := (pktType >> 1) & 0x03
			topic, rest, ok := parsePublishPayload(payload, qos)
			if ok {
				c.dispatchMessage(topic, rest)
			}

		case mqttSubAck:
			if len(payload) >= 3 {
				pktID := binary.BigEndian.Uint16(payload[0:2])
				var subErr error
				if payload[2] == 0x80 {
					subErr = errors.New("hardware/mqtt: broker rejected subscription")
				}
				c.ackMu.Lock()
				if ch, ok := c.subAckChans[pktID]; ok {
					ch <- subErr
				}
				c.ackMu.Unlock()
			}

		case mqttPingResp:
			select {
			case c.pingCh <- struct{}{}:
			default:
			}
		}
	}
}

func (c *MQTTClient) dispatchMessage(topic string, payload []byte) {
	c.subMu.RLock()
	var handlers []MQTTMessageHandler
	for _, s := range c.subs {
		if s.handler != nil && mqttTopicMatch(s.filter, topic) {
			handlers = append(handlers, s.handler)
		}
	}
	c.subMu.RUnlock()

	for _, fn := range handlers {
		cp := make([]byte, len(payload))
		copy(cp, payload)
		fn(topic, cp)
	}
}

func parsePublishPayload(payload []byte, qos byte) (string, []byte, bool) {
	if len(payload) < 2 {
		return "", nil, false
	}
	topicLen := int(binary.BigEndian.Uint16(payload[0:2]))
	pos := 2 + topicLen
	if pos > len(payload) {
		return "", nil, false
	}
	topic := string(payload[2:pos])
	if qos > 0 {
		if pos+2 > len(payload) {
			return "", nil, false
		}
		pos += 2 // skip Packet Identifier for QoS 1/2
	}
	return topic, payload[pos:], true
}

func writeMQTTString(b *bytes.Buffer, s string) {
	_ = binary.Write(b, binary.BigEndian, uint16(len(s)))
	b.WriteString(s)
}

func writeMQTTPacket(bw *bufio.Writer, header byte, body []byte) error {
	if err := bw.WriteByte(header); err != nil {
		return err
	}
	if err := writeRemainingLength(bw, len(body)); err != nil {
		return err
	}
	if len(body) > 0 {
		if _, err := bw.Write(body); err != nil {
			return err
		}
	}
	return bw.Flush()
}

func readMQTTPacket(br *bufio.Reader) (byte, []byte, error) {
	header, err := br.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	remLen, err := readRemainingLength(br)
	if err != nil {
		return 0, nil, err
	}
	if remLen == 0 {
		return header, nil, nil
	}
	payload := make([]byte, remLen)
	if _, err := io.ReadFull(br, payload); err != nil {
		return 0, nil, err
	}
	return header, payload, nil
}

func writeRemainingLength(bw *bufio.Writer, length int) error {
	for {
		encoded := byte(length % 128)
		length /= 128
		if length > 0 {
			encoded |= 0x80
		}
		if err := bw.WriteByte(encoded); err != nil {
			return err
		}
		if length == 0 {
			break
		}
	}
	return nil
}

func readRemainingLength(br *bufio.Reader) (int, error) {
	multiplier := 1
	value := 0
	for i := 0; i < 4; i++ {
		b, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		value += int(b&0x7F) * multiplier
		if b&0x80 == 0 {
			return value, nil
		}
		multiplier *= 128
	}
	return 0, errors.New("hardware/mqtt: malformed remaining length")
}

// mqttTopicMatch matches an MQTT topic against a filter supporting '+' and '#' wildcards.
func mqttTopicMatch(filter, topic string) bool {
	if filter == topic || filter == "#" {
		return true
	}
	fParts := strings.Split(filter, "/")
	tParts := strings.Split(topic, "/")
	for i := 0; i < len(fParts); i++ {
		if fParts[i] == "#" {
			return true
		}
		if i >= len(tParts) {
			return false
		}
		if fParts[i] != "+" && fParts[i] != tParts[i] {
			return false
		}
	}
	return len(fParts) == len(tParts)
}
