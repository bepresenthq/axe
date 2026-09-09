package build

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/k-kohey/axe/internal/procgroup"
)

// commandRunner is the production implementation of Runner that executes
// real xcodebuild commands via procgroup.
type commandRunner struct{}

// NewRunner returns a Runner that executes real xcodebuild commands.
func NewRunner() Runner { return &commandRunner{} }

func (r *commandRunner) FetchBuildSettings(ctx context.Context, args []string) ([]byte, error) {
	fmt.Fprintln(os.Stderr, "[runbp] Resolving packages and reading build settings")
	return run(ctx, args)
}

func (r *commandRunner) Build(ctx context.Context, args []string) ([]byte, error) {
	return run(ctx, args)
}

func run(ctx context.Context, args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command args")
	}
	var output bytes.Buffer
	cmd := procgroup.Command(ctx, args[0], args[1:]...)
	cmd.Stdout = io.MultiWriter(&output, os.Stderr)
	cmd.Stderr = cmd.Stdout
	err := cmd.Run()
	return output.Bytes(), err
}
