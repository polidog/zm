package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/polidog/zm/internal/catalog"
)

// picker はエフェクト選択。←→ でグループ、文字を打つと名前で絞り込み、↑↓ で選ぶ。
type picker struct {
	cat    *catalog.Catalog
	groups []string // 先頭は "ALL"
	group  int
	filter string
	list   []*catalog.Effect
	cursor int
}

func newPicker(cat *catalog.Catalog) picker {
	p := picker{cat: cat, groups: append([]string{"ALL"}, cat.Groups()...)}
	p.refresh()
	return p
}

func (p *picker) refresh() {
	p.list = p.list[:0]
	q := strings.ToLower(p.filter)
	for i := range p.cat.Effects {
		e := &p.cat.Effects[i]
		if p.group > 0 && e.GroupName != p.groups[p.group] {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Name), q) {
			continue
		}
		p.list = append(p.list, e)
	}
	p.cursor = min(p.cursor, max(0, len(p.list)-1))
}

func (p *picker) selectID(id string) {
	for i, e := range p.list {
		if e.ID == id {
			p.cursor = i
			return
		}
	}
}

func (p *picker) selected() *catalog.Effect {
	if p.cursor < len(p.list) {
		return p.list[p.cursor]
	}
	return nil
}

func (p *picker) key(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "up":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down":
		if p.cursor < len(p.list)-1 {
			p.cursor++
		}
	case "left":
		p.group = (p.group + len(p.groups) - 1) % len(p.groups)
		p.cursor = 0
		p.refresh()
	case "right", "tab":
		p.group = (p.group + 1) % len(p.groups)
		p.cursor = 0
		p.refresh()
	case "backspace":
		if p.filter != "" {
			r := []rune(p.filter)
			p.filter = string(r[:len(r)-1])
			p.cursor = 0
			p.refresh()
		}
	default:
		if msg.Text != "" && msg.Mod == 0 || msg.Text != "" && msg.Mod == tea.ModShift {
			p.filter += msg.Text
			p.cursor = 0
			p.refresh()
		}
	}
}
