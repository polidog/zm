// Package midi は ALSA rawmidi (/dev/snd/midiCxDy) で SysEx を送受信する (L1)。
//
// 外部ライブラリを使わず、デバイスファイルを O_NONBLOCK で開いて直接読み書きする。
// O_NONBLOCK で開いたファイルはランタイムの poller に載るので、読み出しは goroutine 1 本で
// ブロックして待ち、Close で確実に抜ける。
package midi

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Port struct {
	Card   int
	Device int
	Name   string
}

func (p Port) Path() string { return fmt.Sprintf("/dev/snd/midiC%dD%d", p.Card, p.Device) }

var (
	cardLine = regexp.MustCompile(`^\s*(\d+)\s+\[[^\]]*\]:\s*(.*)$`)
	midiFile = regexp.MustCompile(`^midiC(\d+)D(\d+)$`)
)

// ListPorts は rawmidi デバイスを持つサウンドカードを列挙する。
func ListPorts() ([]Port, error) {
	names := map[int]string{}
	if f, err := os.Open("/proc/asound/cards"); err == nil {
		s := bufio.NewScanner(f)
		for s.Scan() {
			if m := cardLine.FindStringSubmatch(s.Text()); m != nil {
				n, _ := strconv.Atoi(m[1])
				names[n] = strings.TrimSpace(m[2])
			}
		}
		f.Close()
	}
	entries, err := os.ReadDir("/dev/snd")
	if err != nil {
		return nil, err
	}
	var ports []Port
	for _, e := range entries {
		m := midiFile.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		card, _ := strconv.Atoi(m[1])
		dev, _ := strconv.Atoi(m[2])
		name, ok := names[card]
		if !ok {
			name = "?"
		}
		ports = append(ports, Port{Card: card, Device: dev, Name: name})
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Card != ports[j].Card {
			return ports[i].Card < ports[j].Card
		}
		return ports[i].Device < ports[j].Device
	})
	return ports, nil
}

var zoomName = regexp.MustCompile(`(?i)zoom`)

func FindZoomPort() (Port, bool) {
	ports, _ := ListPorts()
	for _, p := range ports {
		if zoomName.MatchString(p.Name) {
			return p, true
		}
	}
	return Port{}, false
}

// Conn は開いている rawmidi ポート。受信は goroutine で SysEx 単位に切り分け、Messages に流す。
//
// Messages を読むのは 1 つの goroutine だけにする（Request もそこから読む）。
type Conn struct {
	Port Port
	f    *os.File
	msgs chan []byte
	done chan struct{}

	mu   sync.Mutex
	err  error
	once sync.Once

	// Unmatched は Request / Drain が読み捨てた SysEx を受け取る（nil なら捨てる）。
	// ペダルが自分から送ってくる通知 (64 12 00 など) を拾うのに使う。
	Unmatched func([]byte)
}

func Open(p Port) (*Conn, error) {
	f, err := os.OpenFile(p.Path(), os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	c := &Conn{Port: p, f: f, msgs: make(chan []byte, 256), done: make(chan struct{})}
	go c.readLoop()
	return c, nil
}

func (c *Conn) readLoop() {
	defer close(c.msgs)
	defer close(c.done)
	chunk := make([]byte, 4096)
	var buf []byte
	for {
		n, err := c.f.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			for {
				start := bytes.IndexByte(buf, 0xf0)
				if start < 0 {
					buf = buf[:0]
					break
				}
				end := bytes.IndexByte(buf[start:], 0xf7)
				if end < 0 {
					buf = buf[start:]
					break
				}
				msg := append([]byte(nil), buf[start:start+end+1]...)
				buf = buf[start+end+1:]
				c.msgs <- msg
			}
		}
		if err != nil {
			if !errors.Is(err, os.ErrClosed) {
				c.mu.Lock()
				c.err = err
				c.mu.Unlock()
			}
			return
		}
	}
}

// Messages は受信した SysEx。ポートを閉じると閉じる。
func (c *Conn) Messages() <-chan []byte { return c.msgs }

// Close はポートを閉じ、受信の goroutine が抜けるまで待つ。何度呼んでもよい。
func (c *Conn) Close() error {
	var err error
	c.once.Do(func() {
		err = c.f.Close()
		// 受信側が詰まっていても抜けられるように読み捨てる
		go func() {
			for range c.msgs {
			}
		}()
		<-c.done
	})
	return err
}

func (c *Conn) Send(data []byte) error {
	_, err := c.f.Write(data)
	return err
}

func (c *Conn) readErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return os.ErrClosed
}

func (c *Conn) unmatched(msg []byte) {
	if c.Unmatched != nil {
		c.Unmatched(msg)
	}
}

// Drain は溜まっている受信を Unmatched に流して空にする。
func (c *Conn) Drain() {
	for {
		select {
		case msg, ok := <-c.msgs:
			if !ok {
				return
			}
			c.unmatched(msg)
		default:
			return
		}
	}
}

// Recv は SysEx を 1 つ受け取る。timeout までに来なければ nil, nil。
func (c *Conn) Recv(timeout time.Duration) ([]byte, error) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case msg, ok := <-c.msgs:
		if !ok {
			return nil, c.readErr()
		}
		return msg, nil
	case <-t.C:
		return nil, nil
	}
}

// Request は送って、match に合う SysEx を待つ。合わないもの（前の接続の応答の残りや、
// ペダルが自分から送ってくる通知）は Unmatched に回す。timeout までに来なければ nil, nil。
func (c *Conn) Request(data []byte, timeout time.Duration, match func([]byte) bool) ([]byte, error) {
	c.Drain()
	if err := c.Send(data); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		msg, err := c.Recv(time.Until(deadline))
		if err != nil || msg == nil {
			return nil, err
		}
		if match == nil || match(msg) {
			return msg, nil
		}
		c.unmatched(msg)
	}
}
