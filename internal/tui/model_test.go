package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/polidog/zoom-ai/internal/catalog"
	"github.com/polidog/zoom-ai/internal/device"
	"github.com/polidog/zoom-ai/internal/ops"
	"github.com/polidog/zoom-ai/internal/patch"
	"github.com/polidog/zoom-ai/internal/testdata"
)

type fakePedal struct {
	cur   *patch.Patch
	sets  []string
	saved []int
}

func (f *fakePedal) Model() device.Model             { return device.G2Four }
func (f *fakePedal) Current() (*patch.Patch, error)  { return f.cur.Clone(), nil }
func (f *fakePedal) Patch(int) (*patch.Patch, error) { return &patch.Patch{Name: "Empty"}, nil }
func (f *fakePedal) SetParam(slot, index, value int) error {
	f.sets = append(f.sets, strings.Join([]string{itoa(slot), itoa(index), itoa(value)}, "/"))
	f.cur.Effects[slot].Params[index] = value
	return nil
}
func (f *fakePedal) SetEnabled(slot int, on bool) error { f.cur.Effects[slot].Enabled = on; return nil }
func (f *fakePedal) SetEffect(slot int, id uint32) (*patch.Patch, error) {
	f.cur.Effects[slot] = patch.Effect{ID: id, Enabled: true, Params: make([]int, 12), Extra: "0"}
	return f.cur.Clone(), nil
}
func (f *fakePedal) Save(n int) (ops.SaveResult, error) {
	f.saved = append(f.saved, n)
	return ops.SaveResult{Number: n}, nil
}
func (f *fakePedal) Close() error { return nil }

func itoa(n int) string { return strconv.Itoa(n) }

func setup(t *testing.T) (*fakePedal, Model) {
	t.Helper()
	path := filepath.Join(testdata.Catalog(), "g2four.json")
	if _, err := os.Stat(path); err != nil {
		t.Skip("catalog/g2four.json がない")
	}
	cat, err := catalog.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := cat.Resolve("TS Drive")
	var id uint32
	for _, c := range ts.ID[2:] {
		id = id*16 + uint32(strings.IndexRune("0123456789abcdef", c))
	}
	effects := make([]patch.Effect, 8)
	for i := range effects {
		effects[i] = patch.Effect{Enabled: true, Params: make([]int, 12), Extra: "0"}
	}
	effects[0].ID = id
	effects[0].Params[0] = 5
	f := &fakePedal{cur: &patch.Patch{Name: "TEST", Effects: effects}}
	m := New(f, cat)
	m = step(t, m, m.Init())
	return f, m
}

// step は cmd を同期で走らせ、返ってきたメッセージを Update に通す（返ってきた cmd も続けて走らせる）。
func step(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				m = step(t, m, c)
			}
			return m
		}
		if msg == nil {
			return m
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(Model)
	}
	return m
}

func press(m Model, key string) (Model, tea.Cmd) {
	k := tea.KeyPressMsg{}
	switch key {
	case "up":
		k.Code = tea.KeyUp
	case "down":
		k.Code = tea.KeyDown
	case "enter":
		k.Code = tea.KeyEnter
	case "space":
		k.Code = tea.KeySpace
	default:
		k.Code, k.Text = rune(key[0]), key
	}
	next, cmd := m.Update(k)
	return next.(Model), cmd
}

// 押しっぱなしで + を何度も押しても、送るのは 1 本ずつで、最後の値に追いつく。
func TestChangesAreCoalesced(t *testing.T) {
	f, m := setup(t)
	var first tea.Cmd
	for i := range 5 {
		var cmd tea.Cmd
		m, cmd = press(m, "+")
		if i == 0 {
			first = cmd
		} else if cmd != nil {
			t.Fatalf("%d 回目: 送っている間に次を送り始めた", i+1)
		}
	}
	if got := m.value(0, 0); got != 10 {
		t.Fatalf("画面の値 %d", got)
	}
	m = step(t, m, first)
	if strings.Join(f.sets, " ") != "0/0/6 0/0/10" {
		t.Errorf("送った値: %v", f.sets)
	}
	if len(m.pending) != 0 || m.sending != nil {
		t.Errorf("送り残し: %v", m.pending)
	}
}

func TestValueIsClamped(t *testing.T) {
	f, m := setup(t)
	for range 10 {
		var cmd tea.Cmd
		m, cmd = press(m, "-")
		m = step(t, m, cmd)
	}
	if m.value(0, 0) != 0 || strings.Join(f.sets, " ") != "0/0/4 0/0/3 0/0/2 0/0/1 0/0/0" {
		t.Errorf("value=%d sets=%v", m.value(0, 0), f.sets)
	}
}

// ペダル側でつまみを回した通知が画面に映る。送っていない値はその上に残る。
func TestNoticeUpdatesScreen(t *testing.T) {
	_, m := setup(t)
	p := m.patch.Clone()
	p.Effects[0].Params[1] = 42
	next, _ := m.Update(noticeMsg{p})
	m = next.(Model)
	if m.value(0, 1) != 42 {
		t.Errorf("通知が映らない: %d", m.value(0, 1))
	}
	m.pending[paramKey{0, 1}] = 7
	next, _ = m.Update(noticeMsg{p})
	if next.(Model).value(0, 1) != 7 {
		t.Error("送っていない値が通知で消えた")
	}
}

func TestPickerSetsEffect(t *testing.T) {
	f, m := setup(t)
	m, _ = press(m, "down")
	m, _ = press(m, "e")
	for _, c := range "chorus" {
		m, _ = press(m, string(c))
	}
	if e := m.picker.selected(); e == nil || !strings.EqualFold(e.Name, "Chorus") {
		t.Fatalf("選べない: %+v", e)
	}
	m, cmd := press(m, "enter")
	m = step(t, m, cmd)
	if _, e := m.effect(1); e == nil || e.Name != "Chorus" {
		t.Errorf("差し替わらない")
	}
	if f.cur.Effects[1].ID == 0 {
		t.Error("ペダルに送っていない")
	}
}

func TestSaveNeedsConfirmation(t *testing.T) {
	f, m := setup(t)
	m, _ = press(m, "s")
	for _, c := range "263" {
		m, _ = press(m, string(c))
	}
	m, cmd := press(m, "enter")
	m = step(t, m, cmd)
	if m.mode != modeSaveConfirm || m.target != "Empty" {
		t.Fatalf("確認に進まない: mode=%d target=%q", m.mode, m.target)
	}
	if len(f.saved) != 0 {
		t.Fatal("確認の前に保存した")
	}
	m, cmd = press(m, "n")
	m = step(t, m, cmd)
	if len(f.saved) != 0 {
		t.Fatal("n で保存した")
	}
	m, _ = press(m, "s")
	for _, c := range "263" {
		m, _ = press(m, string(c))
	}
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)
	m, cmd = press(m, "y")
	step(t, m, cmd)
	if len(f.saved) != 1 || f.saved[0] != 263 {
		t.Errorf("保存: %v", f.saved)
	}
}

func TestToggleAndView(t *testing.T) {
	f, m := setup(t)
	m, cmd := press(m, "space")
	m = step(t, m, cmd)
	if f.cur.Effects[0].Enabled {
		t.Error("OFF にならない")
	}
	out := m.render()
	for _, want := range []string{"TEST", "TS Drive", "Gain", "(empty)"} {
		if !strings.Contains(out, want) {
			t.Errorf("画面に %q がない:\n%s", want, out)
		}
	}
}
