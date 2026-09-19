// Package dss reads the subset of the OpenDSS script language that the CSIRO
// low-voltage feeder models use, and turns a circuit into the engine's
// network model.
//
// The subset: `new` for circuits, linecodes, lines, loads, transformers and
// reactors; `redirect`; `~` continuation lines; `!` and `//` comments; and
// `[a | b c]` matrices. Names and keys are case-insensitive, as in OpenDSS. A
// property or element class outside the subset is an error, not a silent
// skip, so a feeder that needs more than the subset cannot be mis-modelled.
package dss

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Arg is one `key=value` property of a command, or a positional value with an
// empty key. Keys are lower-cased.
type Arg struct {
	Key   string
	Value string
}

// Command is one statement, with its continuation lines joined on.
type Command struct {
	// Verb is the first word, lower-cased: "new", "redirect", "set".
	Verb string
	Args []Arg
	File string
	Line int
}

// errorf prefixes an error with the command's position.
func (c Command) errorf(format string, args ...any) error {
	return fmt.Errorf("%s:%d: %s", c.File, c.Line, fmt.Sprintf(format, args...))
}

// Parse reads the statements of one script. file names the script in errors.
func Parse(file string, r io.Reader) ([]Command, error) {
	var cmds []Command
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(stripComment(sc.Text()))
		if text == "" {
			continue
		}
		verb, rest := splitVerb(text)
		args, err := tokenise(rest)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, line, err)
		}
		if verb == "~" || verb == "more" {
			if len(cmds) == 0 {
				return nil, fmt.Errorf("%s:%d: continuation line with no command before it", file, line)
			}
			last := &cmds[len(cmds)-1]
			last.Args = append(last.Args, args...)
			continue
		}
		cmds = append(cmds, Command{Verb: verb, Args: args, File: file, Line: line})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return cmds, nil
}

// stripComment removes a `//` line comment and anything from `!` onwards. A
// `!` inside quotes is text.
func stripComment(s string) string {
	if strings.HasPrefix(strings.TrimSpace(s), "//") {
		return ""
	}
	var quote rune
	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '!':
			return s[:i]
		}
	}
	return s
}

// splitVerb separates the first word from the rest. `~` needs no space after
// it.
func splitVerb(s string) (verb, rest string) {
	if s[0] == '~' {
		return "~", s[1:]
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return strings.ToLower(s[:i]), s[i:]
	}
	return strings.ToLower(s), ""
}

// closers maps each opening delimiter of a grouped value to its closer.
var closers = map[byte]byte{'"': '"', '\'': '\'', '[': ']', '(': ')', '{': '}'}

func isSeparator(b byte) bool {
	return b == ' ' || b == '\t' || b == ','
}

// tokenise splits the text after the verb into arguments. A value that starts
// with a quote or bracket runs to the matching closer and may contain spaces.
func tokenise(s string) ([]Arg, error) {
	var args []Arg
	i := 0
	for {
		for i < len(s) && isSeparator(s[i]) {
			i++
		}
		if i == len(s) {
			return args, nil
		}

		// A bare word, up to `=` or a separator.
		start := i
		for i < len(s) && !isSeparator(s[i]) && s[i] != '=' {
			i++
		}
		word := s[start:i]
		if i == len(s) || s[i] != '=' {
			args = append(args, Arg{Value: word})
			continue
		}
		i++ // the '='

		var value string
		if closer, grouped := closers[at(s, i)]; grouped {
			end := strings.IndexByte(s[i+1:], closer)
			if end < 0 {
				return nil, fmt.Errorf("%s: missing closing %q", word, string(closer))
			}
			value = s[i+1 : i+1+end]
			i += end + 2
		} else {
			start = i
			for i < len(s) && !isSeparator(s[i]) {
				i++
			}
			value = s[start:i]
		}
		args = append(args, Arg{Key: strings.ToLower(word), Value: strings.TrimSpace(value)})
	}
}

// at returns s[i], or 0 past the end.
func at(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return 0
}
