package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bootstrap"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localbootstrap"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/locallogin"
	"github.com/hvritual/yunka.io/framework/core/identity"
	_ "modernc.org/sqlite"
)

const organizationID = "org-yu29-e2e"

type manifest struct {
	OrganizationID           string `json:"organizationId"`
	AdminUserID              string `json:"adminUserId"`
	AdminPassword            string `json:"adminPassword"`
	MemberUserID             string `json:"memberUserId"`
	MemberPassword           string `json:"memberPassword"`
	MemberUserRevision       int64  `json:"memberUserRevision"`
	MemberCredentialRevision int64  `json:"memberCredentialRevision"`
	LocalAuthJWTKey          string `json:"localAuthJwtKey"`
	BFFAssertionKey          string `json:"bffAssertionKey"`
}

func main() {
	var databasePath string
	var vaultPath string
	var manifestPath string
	flag.StringVar(&databasePath, "db", "", "SQLite database path")
	flag.StringVar(&vaultPath, "vault", "", "temporary Obsidian vault path")
	flag.StringVar(&manifestPath, "manifest", "", "output fixture manifest path")
	flag.Parse()

	if databasePath == "" || vaultPath == "" || manifestPath == "" {
		fatal(errors.New("-db, -vault and -manifest are required"))
	}
	for _, path := range []string{databasePath, manifestPath} {
		if err := os.MkdirAll(filepath.Dir(filepath.Clean(path)), 0o755); err != nil {
			fatal(err)
		}
	}
	_ = os.Remove(databasePath)
	_ = os.Remove(manifestPath)

	jwtKey := randomSecret(32)
	bffKey := randomSecret(32)
	adminPassword := "YU29-admin-" + randomSecret(18)
	memberPassword := "YU29-member-" + randomSecret(18)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	application, err := bootstrap.New(ctx, bootstrap.Config{
		HTTPAddress:              "127.0.0.1:0",
		GRPCAddress:              "127.0.0.1:0",
		DatabasePath:             databasePath,
		ObsidianVault:            vaultPath,
		BFFOrganizationID:        organizationID,
		BFFAssertionKey:          bffKey,
		LocalAuthJWTSigningKey:   jwtKey,
		RuntimeEnvironment:       bootstrap.RuntimeEnvironmentProduction,
		BootstrapMode:            bootstrap.BootstrapModeDisabled,
		LegacyLocalAPIKeyEnabled: false,
	})
	if err != nil {
		fatal(fmt.Errorf("start fixture application: %w", err))
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer closeCancel()
		if err := application.Close(closeCtx); err != nil {
			fatal(fmt.Errorf("close fixture application: %w", err))
		}
	}()

	if err := insertOrganization(ctx, databasePath); err != nil {
		fatal(err)
	}
	admin, err := application.AdministratorBootstrap().Initialize(ctx, localbootstrap.InitializeInput{
		OrganizationID: organizationID,
		DisplayName:    "YU-29 System Administrator",
		Email:          "yu29-admin@example.test",
		Password:       []byte(adminPassword),
	})
	if err != nil {
		fatal(fmt.Errorf("bootstrap administrator: %w", err))
	}
	login, err := application.LocalAuthentication().Login(ctx, locallogin.LoginInput{
		OrganizationID: organizationID,
		UserID:         admin.UserID,
		Password:       []byte(adminPassword),
	})
	if err != nil {
		fatal(fmt.Errorf("login administrator for fixture setup: %w", err))
	}
	principal, err := application.LocalAuthentication().VerifyAccessToken(ctx, login.AccessToken)
	if err != nil {
		fatal(fmt.Errorf("verify fixture administrator: %w", err))
	}
	member, err := application.MemberAdministration().Create(identity.WithPrincipal(ctx, principal), localmemberadmin.CreateInput{
		DisplayName: "YU-29 Ordinary Member",
		Email:       "yu29-member@example.test",
		Password:    []byte(memberPassword),
	})
	if err != nil {
		fatal(fmt.Errorf("create ordinary member through YU-20: %w", err))
	}

	output := manifest{
		OrganizationID: organizationID,
		AdminUserID: admin.UserID,
		AdminPassword: adminPassword,
		MemberUserID: member.UserID,
		MemberPassword: memberPassword,
		MemberUserRevision: member.UserRevision,
		MemberCredentialRevision: member.CredentialRevision,
		LocalAuthJWTKey: jwtKey,
		BFFAssertionKey: bffKey,
	}
	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fatal(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		fatal(fmt.Errorf("write fixture manifest: %w", err))
	}
	fmt.Printf("YU-29 fixture ready: organization=%s admin=%s member=%s\n", organizationID, admin.UserID, member.UserID)
}

func insertOrganization(ctx context.Context, databasePath string) error {
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return fmt.Errorf("open fixture SQLite: %w", err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		return fmt.Errorf("configure fixture SQLite: %w", err)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO organizations (id, slug, name, status) VALUES (?, ?, ?, 'active')`, organizationID, "yu29-e2e", "YU-29 E2E Organization")
	if err != nil {
		return fmt.Errorf("insert fixture organization: %w", err)
	}
	return nil
}

func randomSecret(bytes int) string {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		fatal(fmt.Errorf("generate fixture secret: %w", err))
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "YU-29 fixture:", err)
	os.Exit(1)
}
