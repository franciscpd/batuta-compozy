package publication

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type ExecutableResolver struct{}

func (ExecutableResolver) Resolve(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("publication: executable name is required")
	}
	resolved, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("publication: resolve executable %q: %w", name, err)
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("publication: resolve absolute executable path: %w", err)
	}
	return filepath.Clean(absolute), nil
}
