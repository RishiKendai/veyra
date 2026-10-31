// Package cli parses and validates the veyra command line.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/RishiKendai/veyra/internal/model"
)

const Usage = `veyra — Visual Exploration & Yield-based Recognition Assistant
Find duplicate and visually similar images and videos.

Usage:
  veyra                                  interactive menu
  veyra -t <video|image> [-r] <directory>
  veyra help

Options:
  -t    Media type to scan: video or image (required)
  -r    Recursively scan nested directories
  -h    Show help (same as 'veyra help')

Examples:
  veyra                                  pick what to scan from a menu
  veyra -t image .                       scan the current folder
  veyra -t image -r ~/Pictures
  veyra -t video -r /Volumes/Media
`

// Options is the validated result of parsing.
type Options struct {
	Type        model.MediaType
	Recursive   bool
	Root        string // absolute, cleaned
	Help        bool
	Version     bool
	Interactive bool // no arguments: ask what to scan
}

// Parse parses args (without the program name) and validates the scan root.
// Flags may appear before or after the directory; "--" ends flag parsing.
// No arguments at all selects the interactive menu; "help" and "version" are
// accepted only as the single argument.
func Parse(args []string) (Options, error) {
	if len(args) == 0 {
		return Options{Interactive: true}, nil
	}
	if len(args) == 1 {
		switch args[0] {
		case "help":
			return Options{Help: true}, nil
		case "version", "--version":
			return Options{Version: true}, nil
		}
	}
	var (
		o        Options
		typ      string
		haveType bool
		pos      []string
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			pos = append(pos, args[i+1:]...)
			i = len(args)
		case a == "-h" || a == "--help":
			o.Help = true
		case a == "-r":
			o.Recursive = true
		case a == "-t":
			if i+1 >= len(args) {
				return o, errors.New("flag -t requires a value (video or image)")
			}
			i++
			typ, haveType = args[i], true
		case strings.HasPrefix(a, "-t="):
			typ, haveType = strings.TrimPrefix(a, "-t="), true
		case len(a) > 1 && a[0] == '-':
			return o, fmt.Errorf("unknown flag %q", a)
		default:
			pos = append(pos, a)
		}
	}
	if o.Help {
		return o, nil // -h needs no other arguments
	}
	if !haveType {
		return o, errors.New("missing required flag -t (video or image)")
	}
	switch model.MediaType(typ) {
	case model.Video, model.Image:
		o.Type = model.MediaType(typ)
	default:
		return o, fmt.Errorf("invalid type %q: must be video or image", typ)
	}
	switch len(pos) {
	case 0:
		return o, errors.New("missing directory argument")
	case 1:
	default:
		return o, fmt.Errorf("unexpected extra arguments: %s", strings.Join(pos[1:], " "))
	}
	root, err := ValidateDir(pos[0])
	if err != nil {
		return o, err
	}
	o.Root = root
	return o, nil
}

// ValidateDir resolves p to an absolute path and checks that it is a
// readable directory. The interactive menu uses it too, so both entry points
// report problems identically.
func ValidateDir(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", p, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("directory does not exist: %s", p)
		}
		return "", fmt.Errorf("cannot access %s: %w", p, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("not a directory: %s", p)
	}
	d, err := os.Open(abs)
	if err != nil {
		return "", fmt.Errorf("directory not readable: %s: %w", p, err)
	}
	defer d.Close()
	// io.EOF means an empty directory, which is fine.
	if _, err := d.ReadDir(1); err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("directory not readable: %s: %w", p, err)
	}
	return abs, nil
}
