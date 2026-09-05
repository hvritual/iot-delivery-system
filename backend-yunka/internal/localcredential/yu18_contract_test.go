package localcredential

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
)

func TestYU18MigrationRejectsExactColumnsWithoutTenantUserForeignKey(t *testing.T) {
	database := openDatabase(t)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	malformed := strings.Replace(
		credentialSchema,
		",\n    FOREIGN KEY (organization_id, user_id) REFERENCES users(organization_id, id) ON DELETE RESTRICT\n",
		"\n",
		1,
	)
	if malformed == credentialSchema {
		t.Fatal("test fixture failed to remove the credential foreign key")
	}
	if _, err := database.Exec(malformed); err != nil {
		t.Fatalf("create same-column malformed credential schema: %v", err)
	}
	if err := ApplyMigrations(t.Context(), database); err == nil {
		t.Fatal("same-column schema without tenant-bound user foreign key was accepted")
	}
}

func TestYU18CredentialProductionPackageHasNoAuditOrLoggingDependency(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Clean(name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("decode import in %s: %v", name, err)
			}
			if path == "log" || path == "log/slog" || strings.HasSuffix(path, "/audit") {
				t.Fatalf("credential persistence package %s imports forbidden side-channel %q", name, path)
			}
		}
	}
}
