package hardware_test

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gopkin/hardware"
)

func TestMockGPIOPinModesReadsWritesAndToggle(t *testing.T) {
	gpio := hardware.NewMockGPIO()

	if err := gpio.PinMode(-1, hardware.PinOutput); err == nil {
		t.Fatalf("expected error for negative pin")
	}
	if err := gpio.PinMode(17, hardware.PinOutput); err != nil {
		t.Fatalf("PinMode(17, PinOutput) failed: %v", err)
	}
	if gpio.Mode(17) != hardware.PinOutput {
		t.Fatalf("expected PinOutput, got %q", gpio.Mode(17))
	}

	lvl, err := gpio.DigitalRead(17)
	if err != nil || lvl != hardware.Low {
		t.Fatalf("expected initial Low, got (%v, %v)", lvl, err)
	}

	if err := gpio.DigitalWrite(17, hardware.High); err != nil {
		t.Fatalf("DigitalWrite High failed: %v", err)
	}
	lvl, err = gpio.DigitalRead(17)
	if err != nil || lvl != hardware.High {
		t.Fatalf("expected High after write, got (%v, %v)", lvl, err)
	}

	next, err := gpio.Toggle(17)
	if err != nil || next != hardware.Low {
		t.Fatalf("expected Toggle -> Low, got (%v, %v)", next, err)
	}
	next, err = gpio.Toggle(17)
	if err != nil || next != hardware.High {
		t.Fatalf("expected second Toggle -> High, got (%v, %v)", next, err)
	}

	if err := gpio.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := gpio.DigitalWrite(17, hardware.Low); err == nil {
		t.Fatalf("expected error after Close")
	}
}

func TestOpenGPIOAndLinuxSysfsGPIO(t *testing.T) {
	g := hardware.OpenGPIO()
	if g == nil {
		t.Fatalf("OpenGPIO returned nil")
	}
	_ = g.Close()

	// Exercise LinuxSysfsGPIO against a temporary directory simulating /sys/class/gpio.
	sysfs := t.TempDir()
	pinDir := filepath.Join(sysfs, "gpio18")
	if err := os.MkdirAll(pinDir, 0o755); err != nil {
		t.Fatalf("mkdir gpio18 failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pinDir, "value"), []byte("0\n"), 0o644); err != nil {
		t.Fatalf("seed value failed: %v", err)
	}

	sysGPIO := hardware.NewLinuxSysfsGPIO(sysfs)
	if err := sysGPIO.PinMode(18, hardware.PinOutput); err != nil {
		t.Fatalf("LinuxSysfsGPIO.PinMode failed: %v", err)
	}
	dirBytes, err := os.ReadFile(filepath.Join(pinDir, "direction"))
	if err != nil || string(dirBytes) != "out" {
		t.Fatalf("expected direction 'out', got %q (err=%v)", string(dirBytes), err)
	}

	if err := sysGPIO.DigitalWrite(18, hardware.High); err != nil {
		t.Fatalf("LinuxSysfsGPIO.DigitalWrite failed: %v", err)
	}
	lvl, err := sysGPIO.DigitalRead(18)
	if err != nil || lvl != hardware.High {
		t.Fatalf("expected High from sysfs, got (%v, %v)", lvl, err)
	}
	toggled, err := sysGPIO.Toggle(18)
	if err != nil || toggled != hardware.Low {
		t.Fatalf("expected Toggle -> Low from sysfs, got (%v, %v)", toggled, err)
	}
	if err := sysGPIO.Close(); err != nil {
		t.Fatalf("LinuxSysfsGPIO.Close failed: %v", err)
	}
}

func TestMQTTClientFullTCPRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake MQTT broker failed: %v", err)
	}
	defer ln.Close()

	var (
		brokerMu         sync.Mutex
		sawConnect       bool
		sawSubscribe     string
		sawPublishTopic  string
		sawPublishBody   string
		sawPing          bool
		sawDisconnect    bool
		brokerErr        error
		brokerWG         sync.WaitGroup
	)
	brokerWG.Add(1)

	go func() {
		defer brokerWG.Done()
		conn, err := ln.Accept()
		if err != nil {
			brokerMu.Lock()
			brokerErr = err
			brokerMu.Unlock()
			return
		}
		defer conn.Close()

		br := bufio.NewReader(conn)
		bw := bufio.NewWriter(conn)

		for {
			hdr, payload, err := testReadMQTTPacket(br)
			if err != nil {
				return
			}
			switch hdr & 0xF0 {
			case 0x10: // CONNECT
				brokerMu.Lock()
				sawConnect = true
				brokerMu.Unlock()
				// Send CONNACK (Session Present = 0, Return Code = 0 Accepted)
				_ = testWriteMQTTPacket(bw, 0x20, []byte{0x00, 0x00})

			case 0x80: // SUBSCRIBE
				if len(payload) < 5 {
					return
				}
				pktID := binary.BigEndian.Uint16(payload[0:2])
				topicLen := int(binary.BigEndian.Uint16(payload[2:4]))
				topic := string(payload[4 : 4+topicLen])
				brokerMu.Lock()
				sawSubscribe = topic
				brokerMu.Unlock()

				// Send SUBACK (pktID + QoS 0 granted)
				var ack bytes.Buffer
				_ = binary.Write(&ack, binary.BigEndian, pktID)
				ack.WriteByte(0x00)
				_ = testWriteMQTTPacket(bw, 0x90, ack.Bytes())

			case 0x30: // PUBLISH
				topicLen := int(binary.BigEndian.Uint16(payload[0:2]))
				topic := string(payload[2 : 2+topicLen])
				body := string(payload[2+topicLen:])
				brokerMu.Lock()
				sawPublishTopic = topic
				sawPublishBody = body
				brokerMu.Unlock()

				// Echo PUBLISH back to subscriber on matching topic!
				_ = testWriteMQTTPacket(bw, 0x30, payload)

			case 0xC0: // PINGREQ
				brokerMu.Lock()
				sawPing = true
				brokerMu.Unlock()
				_ = testWriteMQTTPacket(bw, 0xD0, nil)

			case 0xE0: // DISCONNECT
				brokerMu.Lock()
				sawDisconnect = true
				brokerMu.Unlock()
				return
			}
		}
	}()

	client := hardware.NewMQTTClient()
	if err := client.Connect(ln.Addr().String(), "gopkin-iot-1", "device", "secret"); err != nil {
		t.Fatalf("MQTT Connect failed: %v", err)
	}
	if !client.IsConnected() {
		t.Fatalf("expected IsConnected() == true")
	}

	receivedCh := make(chan string, 1)
	if err := client.Subscribe("home/+/status", func(topic string, payload []byte) {
		receivedCh <- topic + "=" + string(payload)
	}); err != nil {
		t.Fatalf("MQTT Subscribe failed: %v", err)
	}

	if err := client.Publish("home/light1/status", []byte("ON")); err != nil {
		t.Fatalf("MQTT Publish failed: %v", err)
	}

	select {
	case msg := <-receivedCh:
		if msg != "home/light1/status=ON" {
			t.Fatalf("unexpected subscribed message: %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for incoming MQTT PUBLISH callback")
	}

	if err := client.Ping(); err != nil {
		t.Fatalf("MQTT Ping failed: %v", err)
	}

	if err := client.Disconnect(); err != nil {
		t.Fatalf("MQTT Disconnect failed: %v", err)
	}
	if client.IsConnected() {
		t.Fatalf("expected IsConnected() == false after Disconnect")
	}

	brokerWG.Wait()

	brokerMu.Lock()
	defer brokerMu.Unlock()
	if brokerErr != nil {
		t.Fatalf("fake broker error: %v", brokerErr)
	}
	if !sawConnect || sawSubscribe != "home/+/status" || sawPublishTopic != "home/light1/status" ||
		sawPublishBody != "ON" || !sawPing || !sawDisconnect {
		t.Fatalf("broker did not observe full MQTT lifecycle: connect=%v sub=%q pub=%q:%q ping=%v disc=%v",
			sawConnect, sawSubscribe, sawPublishTopic, sawPublishBody, sawPing, sawDisconnect)
	}
}

func testReadMQTTPacket(br *bufio.Reader) (byte, []byte, error) {
	hdr, err := br.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	mult := 1
	remLen := 0
	for i := 0; i < 4; i++ {
		b, err := br.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		remLen += int(b&0x7F) * mult
		if b&0x80 == 0 {
			break
		}
		mult *= 128
	}
	if remLen == 0 {
		return hdr, nil, nil
	}
	buf := make([]byte, remLen)
	if _, err := io.ReadFull(br, buf); err != nil {
		return 0, nil, err
	}
	return hdr, buf, nil
}

func testWriteMQTTPacket(bw *bufio.Writer, hdr byte, body []byte) error {
	if err := bw.WriteByte(hdr); err != nil {
		return err
	}
	n := len(body)
	for {
		enc := byte(n % 128)
		n /= 128
		if n > 0 {
			enc |= 0x80
		}
		if err := bw.WriteByte(enc); err != nil {
			return err
		}
		if n == 0 {
			break
		}
	}
	if len(body) > 0 {
		if _, err := bw.Write(body); err != nil {
			return err
		}
	}
	return bw.Flush()
}
