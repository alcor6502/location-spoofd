package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
)

// loadConfigFile applies "name value" or "name=value" lines from path as if they were flags,
// so a whole deployment fits in one file and the start command stays one line
// (spoofd -config /usr/local/etc/spoofd.conf). Command-line flags given after -config win.
func loadConfigFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		s := strings.TrimSpace(sc.Text())
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		name, value, ok := strings.Cut(s, "=")
		if !ok {
			name, value, ok = strings.Cut(s, " ")
		}
		if !ok {
			return fmt.Errorf("%s:%d: expected name=value", path, line)
		}
		name = strings.TrimPrefix(strings.TrimSpace(name), "-")
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if flag.Lookup(name) == nil {
			return fmt.Errorf("%s:%d: unknown option %q", path, line, name)
		}
		if err := flag.Set(name, value); err != nil {
			return fmt.Errorf("%s:%d: %s: %v", path, line, name, err)
		}
	}
	return sc.Err()
}
