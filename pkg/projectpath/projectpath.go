package projectpath

import (
	"fmt"
	"os"
	"path/filepath"
)

func GetProjectRoot() (string, error) {
	// Get the executable's path
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to find executable path: %w", err)
	}

	// Navigate up from the executable's path until you reach a directory structure that makes sense (e.g., go.mod exists in the root)
	for {
		// Check if go.mod exists in the current directory
		modPath := filepath.Join(execPath, "go.mod")
		_, err = os.Stat(modPath)
		if err == nil {
			return execPath, nil
		}

		// If go.mod is not found, move up one directory
		execPath = filepath.Dir(execPath)
		if execPath == "/" || execPath == "" {
			return "", fmt.Errorf("unable to find project root")
		}
	}
}
