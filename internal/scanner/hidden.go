package scanner

import "strings"

// hiddenName reports whether a file or folder name marks it hidden on every
// OS: a leading dot (.git, .DS_Store, the macOS "._" companion files).
func hiddenName(name string) bool { return strings.HasPrefix(name, ".") }
