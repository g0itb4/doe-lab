package dss

import (
	"fmt"
	"io/fs"
	"path"
)

// Read parses the script at master in fsys, follows its `redirect` commands,
// and builds the circuit. A redirect path is relative to the script that names
// it.
func Read(fsys fs.FS, master string) (*Circuit, error) {
	cmds, err := expand(fsys, master, nil)
	if err != nil {
		return nil, err
	}
	return Interpret(cmds)
}

// expand parses one script and replaces each redirect with the commands of
// the script it names. open lists the scripts being expanded, to stop a
// redirect loop.
func expand(fsys fs.FS, file string, open []string) ([]Command, error) {
	for _, o := range open {
		if o == file {
			return nil, fmt.Errorf("%s: redirect loop", file)
		}
	}
	f, err := fsys.Open(file)
	if err != nil {
		return nil, fmt.Errorf("open script: %w", err)
	}
	cmds, err := Parse(file, f)
	// The file was only read; a failed close loses nothing.
	_ = f.Close()
	if err != nil {
		return nil, err
	}

	out := make([]Command, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd.Verb != "redirect" {
			out = append(out, cmd)
			continue
		}
		if len(cmd.Args) != 1 || cmd.Args[0].Key != "" {
			return nil, cmd.errorf("redirect: want one file name")
		}
		sub, err := expand(fsys, path.Join(path.Dir(file), cmd.Args[0].Value), append(open, file))
		if err != nil {
			return nil, err
		}
		out = append(out, sub...)
	}
	return out, nil
}
