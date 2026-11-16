package storage

import (
	"path/filepath"
	"runtime"
	"strings"
)

// SameFolder compares lexical folder identities, not symlink targets. Windows
// folds case; Linux must distinguish directories whose names differ by case.
func SameFolder(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
