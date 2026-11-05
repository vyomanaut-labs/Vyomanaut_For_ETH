package main

import (
	"context"
	"errors"
	"testing"
)

// withCleanExtensions swaps the registry for the duration of one test.
func withCleanExtensions(t *testing.T) {
	t.Helper()
	saved := registeredExtensions
	registeredExtensions = nil
	t.Cleanup(func() { registeredExtensions = saved })
}

func TestStartExtensionsRunsEveryRegisteredExtension(t *testing.T) {
	withCleanExtensions(t)
	var started []string
	registerExtension(extension{name: "a", start: func(context.Context, extensionDeps) error {
		started = append(started, "a")
		return nil
	}})
	registerExtension(extension{name: "b", start: func(context.Context, extensionDeps) error {
		started = append(started, "b")
		return nil
	}})

	startExtensions(context.Background(), extensionDeps{})

	if len(started) != 2 || started[0] != "a" || started[1] != "b" {
		t.Fatalf("started = %v, want [a b]", started)
	}
}

func TestStartExtensionsSkipsAFailingExtensionAndContinues(t *testing.T) {
	withCleanExtensions(t)
	var reached bool
	registerExtension(extension{name: "broken", start: func(context.Context, extensionDeps) error {
		return errors.New("boom")
	}})
	registerExtension(extension{name: "ok", start: func(context.Context, extensionDeps) error {
		reached = true
		return nil
	}})

	startExtensions(context.Background(), extensionDeps{})

	if !reached {
		t.Fatal("an extension after a failing one was never started")
	}
}

func TestStartExtensionsWithNoneRegisteredIsANoop(t *testing.T) {
	withCleanExtensions(t)
	startExtensions(context.Background(), extensionDeps{}) // must not panic
}
