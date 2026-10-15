package core

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// documentsDir is the player's Documents folder as Windows has it, which
// OneDrive often moves (…\OneDrive\Documents); a test swaps it.
var documentsDir = func() string {
	if p, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0); err == nil && p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, "Documents")
}
