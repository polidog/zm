package tui

import (
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"github.com/polidog/zoom-ai/internal/ops"
	"github.com/polidog/zoom-ai/internal/patch"
)

// Run は TUI を動かす。MIDI ポートは 1 プロセスしか開けないので、動いている間は MCP や CLI からは使えない。
func Run() error {
	cat, err := ops.Catalog()
	if err != nil {
		return err
	}
	var program atomic.Pointer[tea.Program]
	s, err := openSession(func(p *patch.Patch) {
		if prog := program.Load(); prog != nil {
			prog.Send(noticeMsg{p})
		}
	})
	if err != nil {
		return err
	}
	prog := tea.NewProgram(New(s, cat))
	program.Store(prog)
	_, runErr := prog.Run()
	program.Store(nil)
	// 終了時はエディタモード OFF の応答を待ってから閉じる
	if err := s.Close(); runErr == nil {
		runErr = err
	}
	return runErr
}
