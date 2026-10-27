// Package tui は Bubble Tea の TUI (L4)。
//
// 起動中は 1 本の接続を開いたままにし、ペダル側の操作（64 12 00 の通知）を画面に映す。
// 値の変更は 64 20 ですぐ送るが、送っている間に来た変更はまとめ、最後の値だけを送る（押しっぱなしで詰まらないように）。
// 編集は保存しない。保存は s で番号を確かめてから、上書き前を退避して書く。
package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/polidog/zm/internal/catalog"
	"github.com/polidog/zm/internal/patch"
)

type mode int

const (
	modeNormal mode = iota
	modePicker
	modeSaveNumber
	modeSaveConfirm
)

// paramKey はどのスロットのどのパラメータか（どちらも 0 始まり）。
type paramKey struct{ slot, index int }

type Model struct {
	pedal Pedal
	cat   *catalog.Catalog

	patch *patch.Patch
	slot  int // 0 始まり
	param int // 画面に出るパラメータの何番目か

	// pending はまだペダルに届けていない値。sending が送っている最中のもの。
	pending map[paramKey]int
	sending *paramKey

	mode   mode
	picker picker
	input  string // 保存先の番号
	target string // 保存先の今の名前

	status  string
	err     string
	width   int
	quiting bool
}

// ---- メッセージ ----

type patchMsg struct{ p *patch.Patch }

// noticeMsg はペダルが自分から送ってきた現在パッチ (64 12 00)。
type noticeMsg struct{ p *patch.Patch }

type sentMsg struct {
	key   paramKey
	value int
	err   error
}

type errMsg struct{ err error }

// editFailedMsg は編集が通らなかったとき。画面を先に変えているので読み直す。
type editFailedMsg struct{ err error }

type statusMsg struct {
	text string
	p    *patch.Patch
}

type targetMsg struct {
	number int
	name   string
}

func New(pedal Pedal, cat *catalog.Catalog) Model {
	return Model{pedal: pedal, cat: cat, pending: map[paramKey]int{}, width: 80}
}

func (m Model) Init() tea.Cmd { return m.reload() }

func (m Model) reload() tea.Cmd {
	return func() tea.Msg {
		p, err := m.pedal.Current()
		if err != nil {
			return errMsg{err}
		}
		return patchMsg{p}
	}
}

// ---- 今のスロット ----

func (m Model) userSlots() int { return m.pedal.Model().UserSlots }

func (m Model) effect(slot int) (*patch.Effect, *catalog.Effect) {
	if m.patch == nil || slot >= len(m.patch.Effects) {
		return nil, nil
	}
	fx := &m.patch.Effects[slot]
	if fx.ID == 0 {
		return fx, nil
	}
	return fx, m.cat.Find(fx.ID)
}

func (m Model) visibleParams(slot int) []catalog.Param {
	_, e := m.effect(slot)
	if e == nil {
		return nil
	}
	return e.VisibleParams()
}

// value は送っていない変更も含めた、画面に出す値。
func (m Model) value(slot, index int) int {
	if v, ok := m.pending[paramKey{slot, index}]; ok {
		return v
	}
	fx, _ := m.effect(slot)
	if fx == nil || index >= len(fx.Params) {
		return 0
	}
	return fx.Params[index]
}

func (m *Model) clampParam() {
	n := len(m.visibleParams(m.slot))
	if m.param >= n {
		m.param = max(0, n-1)
	}
}

// ---- 値の送信（間引き） ----

// change は値を変え、送っていなければ送り始める。
func (m *Model) change(delta int) tea.Cmd {
	ps := m.visibleParams(m.slot)
	if len(ps) == 0 {
		m.err = fmt.Sprintf("スロット %d は空", m.slot+1)
		return nil
	}
	p := ps[m.param]
	v := min(p.Max, max(0, m.value(m.slot, m.param)+delta))
	key := paramKey{m.slot, m.param}
	if v == m.value(m.slot, m.param) {
		return nil
	}
	m.pending[key] = v
	return m.flush()
}

func (m *Model) flush() tea.Cmd {
	if m.sending != nil {
		return nil
	}
	for key, v := range m.pending {
		k := key
		m.sending = &k
		return func() tea.Msg {
			return sentMsg{key: k, value: v, err: m.pedal.SetParam(k.slot, k.index, v)}
		}
	}
	return nil
}

func (m *Model) sent(msg sentMsg) tea.Cmd {
	m.sending = nil
	if msg.err != nil {
		delete(m.pending, msg.key)
		m.err = msg.err.Error()
		return tea.Batch(m.reload(), m.flush())
	}
	fx, _ := m.effect(msg.key.slot)
	if fx != nil && msg.key.index < len(fx.Params) {
		fx.Params[msg.key.index] = msg.value
	}
	if m.pending[msg.key] == msg.value {
		delete(m.pending, msg.key)
	}
	return m.flush()
}

// ---- Update ----

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case patchMsg:
		m.patch = msg.p
		m.clampParam()
		return m, nil
	case noticeMsg:
		// 送っていない変更は pending に残っているので、そのまま上に重なる
		m.patch = msg.p
		m.clampParam()
		return m, nil
	case sentMsg:
		return m, m.sent(msg)
	case errMsg:
		m.err = msg.err.Error()
		return m, nil
	case editFailedMsg:
		m.err = msg.err.Error()
		return m, m.reload()
	case statusMsg:
		m.status, m.err = msg.text, ""
		if msg.p != nil {
			m.patch = msg.p
			m.clampParam()
		}
		return m, nil
	case targetMsg:
		m.target = msg.name
		m.mode = modeSaveConfirm
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		m.quiting = true
		return m, tea.Quit
	}
	switch m.mode {
	case modePicker:
		return m.pickerKey(msg)
	case modeSaveNumber:
		return m.saveNumberKey(msg)
	case modeSaveConfirm:
		return m.saveConfirmKey(k)
	}
	m.err, m.status = "", ""
	switch k {
	case "q":
		m.quiting = true
		return m, tea.Quit
	case "up", "k":
		m.slot = (m.slot + m.userSlots() - 1) % m.userSlots()
		m.clampParam()
	case "down", "j":
		m.slot = (m.slot + 1) % m.userSlots()
		m.clampParam()
	case "left", "h":
		if n := len(m.visibleParams(m.slot)); n > 0 {
			m.param = (m.param + n - 1) % n
		}
	case "right", "l":
		if n := len(m.visibleParams(m.slot)); n > 0 {
			m.param = (m.param + 1) % n
		}
	case "+", "=", "shift+up":
		return m, m.change(1)
	case "-", "_", "shift+down":
		return m, m.change(-1)
	case "pgup", "]":
		return m, m.change(10)
	case "pgdown", "[":
		return m, m.change(-10)
	case "space":
		return m, m.toggle()
	case "e":
		m.mode = modePicker
		m.picker = newPicker(m.cat)
		if _, e := m.effect(m.slot); e != nil {
			m.picker.selectID(e.ID)
		}
	case "s":
		m.mode = modeSaveNumber
		m.input = ""
	case "r":
		m.status = "読み直した"
		return m, m.reload()
	}
	return m, nil
}

func (m *Model) toggle() tea.Cmd {
	fx, _ := m.effect(m.slot)
	if fx == nil || fx.ID == 0 {
		m.err = fmt.Sprintf("スロット %d は空", m.slot+1)
		return nil
	}
	slot, on := m.slot, !fx.Enabled
	fx.Enabled = on
	return func() tea.Msg {
		if err := m.pedal.SetEnabled(slot, on); err != nil {
			return editFailedMsg{err}
		}
		return nil
	}
}

// ---- エフェクト選択 ----

func (m Model) pickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
	case "enter":
		e := m.picker.selected()
		if e == nil {
			return m, nil
		}
		m.mode = modeNormal
		slot, name := m.slot, e.Name
		var id uint32
		fmt.Sscanf(e.ID, "0x%x", &id)
		// 差し替えると、そのスロットの送っていない値は意味がなくなる
		for k := range m.pending {
			if k.slot == slot {
				delete(m.pending, k)
			}
		}
		m.param = 0
		return m, func() tea.Msg {
			p, err := m.pedal.SetEffect(slot, id)
			if err != nil {
				return editFailedMsg{err}
			}
			return statusMsg{text: fmt.Sprintf("スロット %d を %s にした（保存していない）", slot+1, name), p: p}
		}
	default:
		m.picker.key(msg)
	}
	return m, nil
}

// ---- 保存 ----

func (m Model) saveNumberKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k := msg.String(); k {
	case "esc":
		m.mode = modeNormal
	case "backspace":
		if m.input != "" {
			m.input = m.input[:len(m.input)-1]
		}
	case "enter":
		n, err := strconv.Atoi(m.input)
		if err != nil || n < 1 || n > m.pedal.Model().Patches {
			m.err = fmt.Sprintf("パッチ番号は 1〜%d", m.pedal.Model().Patches)
			return m, nil
		}
		m.err = ""
		return m, func() tea.Msg {
			p, err := m.pedal.Patch(n - 1)
			if err != nil {
				return errMsg{err}
			}
			return targetMsg{number: n, name: p.Name}
		}
	default:
		if len(msg.Text) == 1 && msg.Text[0] >= '0' && msg.Text[0] <= '9' && len(m.input) < 3 {
			m.input += msg.Text
		}
	}
	return m, nil
}

func (m Model) saveConfirmKey(k string) (tea.Model, tea.Cmd) {
	switch strings.ToLower(k) {
	case "y":
		m.mode = modeNormal
		n, _ := strconv.Atoi(m.input)
		if len(m.pending) > 0 || m.sending != nil {
			m.err = "送っている値があるので、少し待ってから保存する"
			return m, nil
		}
		return m, func() tea.Msg {
			r, err := m.pedal.Save(n)
			if err != nil {
				return errMsg{err}
			}
			return statusMsg{text: fmt.Sprintf("%d に %q を保存した（上書きした %q は %s）", r.Number, r.Saved, r.Overwritten, r.Backup)}
		}
	case "n", "esc":
		m.mode = modeNormal
		m.status = "保存をやめた"
	}
	return m, nil
}
