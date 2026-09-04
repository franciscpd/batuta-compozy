package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/batuta-ai/core/publication"
	"github.com/batuta-ai/compozy/internal/extensionapp"
)

type extensionRunner interface {
	Run(context.Context) error
}

type resolveExecutable func(string) (string, error)
type extensionFactory func(string, string) (extensionRunner, error)

const (
	describeCompozyExecutable = "/__batuta_describe_compozy__"
	describeGitExecutable     = "/__batuta_describe_git__"
)

func run(
	ctx context.Context,
	compozyExecutable string,
	resolve resolveExecutable,
	factory extensionFactory,
) error {
	compozyExecutable = strings.TrimSpace(compozyExecutable)
	if compozyExecutable == "" || !filepath.IsAbs(compozyExecutable) {
		return errors.New("batuta: COMPOZY_EXECUTABLE must be absolute")
	}
	if resolve == nil {
		return errors.New("batuta: executable resolver is required")
	}
	gitExecutable, err := resolve("git")
	if err != nil {
		return fmt.Errorf("batuta: resolve Git: %w", err)
	}
	if !filepath.IsAbs(gitExecutable) {
		return errors.New("batuta: resolved Git executable must be absolute")
	}
	return startExtension(ctx, compozyExecutable, gitExecutable, factory)
}

func runDescribe(ctx context.Context, factory extensionFactory) error {
	return startExtension(ctx, describeCompozyExecutable, describeGitExecutable, factory)
}

func startExtension(ctx context.Context, compozyExecutable, gitExecutable string, factory extensionFactory) error {
	if factory == nil {
		return errors.New("batuta: extension factory is required")
	}
	extension, err := factory(compozyExecutable, gitExecutable)
	if err != nil {
		return fmt.Errorf("batuta: construct extension: %w", err)
	}
	if extension == nil {
		return errors.New("batuta: extension runtime is required")
	}
	return extension.Run(ctx)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	resolver := publication.ExecutableResolver{}
	factory := func(compozyPath, gitPath string) (extensionRunner, error) {
		return extensionapp.New(compozyPath, gitPath)
	}
	var err error
	if len(os.Args) > 1 && strings.TrimSpace(os.Args[len(os.Args)-1]) == "__describe" {
		err = runDescribe(ctx, factory)
	} else {
		err = run(ctx, os.Getenv("COMPOZY_EXECUTABLE"), resolver.Resolve, factory)
	}
	if err != nil {
		log.Fatal(err)
	}
}
