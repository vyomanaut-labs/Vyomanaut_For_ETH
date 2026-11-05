// cmd/microservice — see main.go's package doc comment.
//
// This file is the single extension point for optional, additive background
// features (a chain publisher, a mirror publisher, and so on). It exists so
// that two feature branches can each add a feature WITHOUT both editing
// main.go: main.go calls startExtensions exactly once, and each feature adds
// its own file that registers itself from init().
//
//	// cmd/microservice/myfeature_ext.go
//	func init() {
//	    registerExtension(extension{name: "myfeature", start: startMyFeature})
//	}
//
// Rules every extension follows:
//
//   - start must return quickly. Long-running work goes in a goroutine that
//     watches ctx.Done().
//   - start must be a no-op returning nil when the feature's own
//     VYOMANAUT_*_ENABLED switch is off. A build with no extension enabled
//     behaves exactly like a build with none registered.
//   - A failing start is logged and skipped. It never stops the coordinator:
//     uploads, audits, repair and payments carry on without the extension.
//   - Read configuration from the environment in the extension's own file;
//     do not add fields to config.NetworkProfile or startupConfig for it.
//
// Business logic belongs in internal/ packages; the extension file is wiring
// only (the same rule as the rest of cmd/).
package main

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"log"

	"github.com/vyomanaut-labs/Vyomanaut_V2/internal/config"
)

// extensionDeps is what the coordinator hands every extension. Fields are
// added only when a feature genuinely needs them.
type extensionDeps struct {
	// DB is the request-path pool (role vyomanaut_app).
	DB *sql.DB
	// Profile is the active NetworkProfile.
	Profile config.NetworkProfile
	// SigningKey is the coordinator's Ed25519 key (the one that signs audit
	// receipts and capability tokens). Extensions must treat it as read-only
	// and must never log it.
	SigningKey ed25519.PrivateKey
}

// extension is one optional feature.
type extension struct {
	name  string
	start func(ctx context.Context, deps extensionDeps) error
}

// registeredExtensions is appended to only from init() functions, before
// main runs, so it needs no lock.
var registeredExtensions []extension

// registerExtension adds an extension. Call it from init().
func registerExtension(e extension) {
	registeredExtensions = append(registeredExtensions, e)
}

// startExtensions starts every registered extension. A failure is logged and
// the next extension is tried.
func startExtensions(ctx context.Context, deps extensionDeps) {
	for _, e := range registeredExtensions {
		if err := e.start(ctx, deps); err != nil {
			log.Printf("[EXTENSION] %s did not start (core services unaffected): %v", e.name, err)
			continue
		}
		log.Printf("[EXTENSION] %s started", e.name)
	}
}
