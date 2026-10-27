package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/polidog/zm/internal/catalog"
	"github.com/polidog/zm/internal/ops"
)

var (
	box      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	bold     = lipgloss.NewStyle().Bold(true)
	faint    = lipgloss.NewStyle().Faint(true)
	selected = lipgloss.NewStyle().Reverse(true)
	accent   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	warn     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	on       = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

func meter(v, max int) string {
	if max <= 0 {
		return ""
	}
	n := min(10, (v*10+max/2)/max)
	return accent.Render(strings.Repeat("▮", n)) + faint.Render(strings.Repeat("░", 10-n))
}

// spread は左と右の文字列を width に収まるように両端へ寄せる。
func spread(left, right string, width int) string {
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) innerWidth() int { return max(40, min(m.width, 110)-4) }

func (m Model) render() string {
	if m.quiting {
		return "エディタモードを切って閉じている…\n"
	}
	w := m.innerWidth()
	var lines []string
	if m.patch == nil {
		lines = append(lines, "現在パッチを読んでいる…")
	} else {
		head := bold.Render(m.patch.Name)
		lines = append(lines, spread(head, fmt.Sprintf("vol %d  tempo %d", m.patch.Prm2.PatchVolume, m.patch.Prm2.Tempo), w))
		lines = append(lines, faint.Render(strings.Repeat("─", w)))
		if m.mode == modePicker {
			lines = append(lines, m.renderPicker(w)...)
		} else {
			lines = append(lines, m.renderSlots(w)...)
		}
	}
	lines = append(lines, faint.Render(strings.Repeat("─", w)))
	lines = append(lines, m.footer(w)...)
	return box.Render(strings.Join(lines, "\n")) + "\n"
}

func (m Model) renderSlots(w int) []string {
	var lines []string
	for slot := range m.userSlots() {
		fx, e := m.effect(slot)
		cursor := "  "
		if slot == m.slot {
			cursor = accent.Render("▶ ")
		}
		mark := faint.Render("○")
		if fx != nil && fx.ID != 0 && fx.Enabled {
			mark = on.Render("●")
		}
		name := "(empty)"
		switch {
		case fx == nil || fx.ID == 0:
			mark = " "
		case e != nil:
			name = e.Name
		default:
			name = catalog.FormatID(fx.ID)
		}
		row := fmt.Sprintf("%s%d %s %-12s", cursor, slot+1, mark, name)
		if fx == nil || fx.ID == 0 {
			row = faint.Render(row)
		}
		for i, p := range m.visibleParams(slot) {
			cell := fmt.Sprintf("%s %d", p.Name, m.value(slot, i))
			if _, ok := m.pending[paramKey{slot, i}]; ok {
				cell += "*"
			}
			if slot == m.slot && i == m.param {
				cell = selected.Render(cell)
			}
			row += "  " + cell
		}
		lines = append(lines, lipgloss.NewStyle().MaxWidth(w).Render(row))
		if slot == m.slot {
			if ps := m.visibleParams(slot); len(ps) > 0 {
				p := ps[m.param]
				v := m.value(slot, m.param)
				detail := fmt.Sprintf("      %s %s %d / %d", bold.Render(p.Name), meter(v, p.Max), v, p.Max)
				if ex := strings.TrimSpace(firstNonEmpty(p.ExplanationJa, p.Explanation)); ex != "" {
					detail += "  " + faint.Render(ex)
				}
				lines = append(lines, lipgloss.NewStyle().MaxWidth(w).Render(detail))
			}
		}
	}
	return lines
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

const pickerRows = 14

func (m Model) renderPicker(w int) []string {
	p := m.picker
	var tabs []string
	for i, g := range p.groups {
		if i == p.group {
			tabs = append(tabs, selected.Render(g))
		} else {
			tabs = append(tabs, faint.Render(g))
		}
	}
	lines := []string{
		lipgloss.NewStyle().MaxWidth(w).Render(strings.Join(tabs, " ")),
		fmt.Sprintf("スロット %d に入れるエフェクト  絞り込み: %s", m.slot+1, accent.Render(p.filter+"▏")),
		"",
	}

	start := max(0, min(p.cursor-pickerRows/2, len(p.list)-pickerRows))
	var left []string
	for i := start; i < min(len(p.list), start+pickerRows); i++ {
		e := p.list[i]
		row := fmt.Sprintf("%-12s %s", e.Name, faint.Render(e.GroupName))
		if i == p.cursor {
			row = selected.Render(fmt.Sprintf("%-12s %s", e.Name, e.GroupName))
		}
		left = append(left, row)
	}
	if len(p.list) == 0 {
		left = append(left, faint.Render("（合うものがない）"))
	}
	leftW := 26
	var right []string
	if e := p.selected(); e != nil {
		d := ops.Detail(e)
		rw := max(20, w-leftW-2)
		right = append(right, bold.Render(d.Name)+faint.Render("  "+d.ID))
		right = append(right, lipgloss.NewStyle().Width(rw).Render(d.Description))
		right = append(right, "")
		for _, pd := range d.Params {
			right = append(right, lipgloss.NewStyle().MaxWidth(rw).Render(
				fmt.Sprintf("%s 0-%d (既定 %d) %s", bold.Render(pd.Name), pd.Max, pd.Default, faint.Render(pd.Explanation))))
		}
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftW).Render(strings.Join(left, "\n")),
		"  ",
		strings.Join(right, "\n"))
	return append(lines, strings.Split(body, "\n")...)
}

func (m Model) footer(w int) []string {
	var lines []string
	switch m.mode {
	case modePicker:
		lines = append(lines, "←→ グループ  文字で絞り込み  ↑↓ 選ぶ  enter 差し替え  esc やめる")
	case modeSaveNumber:
		lines = append(lines, fmt.Sprintf("保存先のパッチ番号 (1〜%d): %s", m.pedal.Model().Patches, accent.Render(m.input+"▏")))
		lines = append(lines, faint.Render("enter 確かめる  esc やめる"))
	case modeSaveConfirm:
		lines = append(lines, warn.Render(fmt.Sprintf("いま鳴っている %q を %s（いまは %q）に保存する。上書き前は backups/overwritten/ に退避する。", m.patch.Name, m.input, m.target)))
		lines = append(lines, "y 保存する  n やめる")
	default:
		lines = append(lines,
			"↑↓ スロット  ←→ パラメータ  +/- 値  [ ] 10 ずつ  space ON/OFF",
			"e エフェクト選択  s 保存  r 読み直し  q 終了    "+faint.Render("編集は保存しない"))
	}
	if m.err != "" {
		lines = append(lines, warn.Render(m.err))
	} else if m.status != "" {
		lines = append(lines, accent.Render(m.status))
	}
	for i, l := range lines {
		lines[i] = lipgloss.NewStyle().MaxWidth(w).Render(l)
	}
	return lines
}
