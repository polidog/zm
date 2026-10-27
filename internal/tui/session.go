package tui

import (
	"sync"
	"time"

	"github.com/polidog/zoom-ai/internal/device"
	"github.com/polidog/zoom-ai/internal/ops"
	"github.com/polidog/zoom-ai/internal/patch"
)

// Pedal は TUI がペダルにやってほしいこと。テストでは偽物に差し替える。
type Pedal interface {
	Model() device.Model
	Current() (*patch.Patch, error)
	Patch(n int) (*patch.Patch, error)
	SetParam(slot, index, value int) error
	SetEnabled(slot int, on bool) error
	SetEffect(slot int, id uint32) (*patch.Patch, error)
	// Save は number（1 始まり）に保存する。上書き前を退避する。
	Save(number int) (ops.SaveResult, error)
	Close() error
}

// session は TUI の間ずっと開いておく 1 本の接続。
//
// ポートからの受信を読むのは 1 か所だけにしたいので、要求も通知の待ち受けも mu の中でやる。
// 要求の途中に届いた通知 (64 12 00) は Unmatched で拾い、空いている間は watch が短く待って拾う。
type session struct {
	mu     sync.Mutex
	z      *device.Zoom
	notify func(*patch.Patch)
	stop   chan struct{}
	done   chan struct{}
}

// openSession は編集用に開き、通知を notify に渡し始める。
func openSession(notify func(*patch.Patch)) (*session, error) {
	z, err := device.Open(true)
	if err != nil {
		return nil, err
	}
	s := &session{z: z, notify: notify, stop: make(chan struct{}), done: make(chan struct{})}
	z.Conn.Unmatched = s.onMessage
	go s.watch()
	return s, nil
}

func (s *session) onMessage(msg []byte) {
	if !device.IsPatchNotice(msg) {
		return
	}
	if p, err := device.DecodeSysex(msg); err == nil {
		s.notify(p)
	}
}

func (s *session) watch() {
	defer close(s.done)
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		s.mu.Lock()
		msg, err := s.z.Conn.Recv(30 * time.Millisecond)
		s.mu.Unlock()
		if err != nil {
			return
		}
		if msg != nil {
			s.onMessage(msg)
		}
	}
}

func (s *session) do(fn func(z *device.Zoom) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(s.z)
}

func (s *session) Model() device.Model { return s.z.Model }

func (s *session) Current() (p *patch.Patch, err error) {
	err = s.do(func(z *device.Zoom) error { p, err = z.CurrentPatch(); return err })
	return
}

func (s *session) Patch(n int) (p *patch.Patch, err error) {
	err = s.do(func(z *device.Zoom) error { p, err = z.Patch(n); return err })
	return
}

func (s *session) SetParam(slot, index, value int) error {
	return s.do(func(z *device.Zoom) error { return z.SetParam(slot, index, value) })
}

func (s *session) SetEnabled(slot int, on bool) error {
	return s.do(func(z *device.Zoom) error { return z.SetEnabled(slot, on) })
}

func (s *session) SetEffect(slot int, id uint32) (p *patch.Patch, err error) {
	err = s.do(func(z *device.Zoom) error { p, err = z.SetEffect(slot, id); return err })
	return
}

func (s *session) Save(number int) (r ops.SaveResult, err error) {
	err = s.do(func(z *device.Zoom) error { r, err = ops.Save(z, number); return err })
	return
}

// Close は待ち受けを止め、エディタモード OFF の応答を待ってから閉じる。
func (s *session) Close() error {
	close(s.stop)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.z.Close()
}
