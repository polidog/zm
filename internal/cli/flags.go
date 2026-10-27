package cli

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/polidog/zoom-ai/internal/ops"
	"github.com/polidog/zoom-ai/internal/paths"
)

// parse はフラグと位置引数が混ざっていても読む（標準の flag は最初の位置引数で止まるので）。
func parse(fs *flag.FlagSet, args []string) []string {
	var pos []string
	for {
		fs.Parse(args)
		args = fs.Args()
		if len(args) == 0 {
			return pos
		}
		if args[0] == "--" {
			return append(pos, args[1:]...)
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func newFlags(name, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.StringVar(&paths.Dir, "dir", paths.Dir, "backups/ と catalog/ の置き場（省略で $ZM_DIR か ~/.config/zm）")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: zm %s\n", usage)
		fs.PrintDefaults()
	}
	return fs
}

func printJSON(v any) error {
	b, err := ops.JSON(v)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func atoi(name, s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s は数字: %q", name, s)
	}
	return n, nil
}

func want(args []string, min, max int, usage string) error {
	if len(args) < min || len(args) > max {
		return fmt.Errorf("usage: zm %s", usage)
	}
	return nil
}

// bar は値を 10 マスで描く。
func bar(v, max int) string {
	if max <= 0 {
		return ""
	}
	n := min(10, (v*10+max/2)/max)
	return strings.Repeat("▮", n) + strings.Repeat("░", 10-n)
}
