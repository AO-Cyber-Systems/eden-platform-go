//go:build dev

package main

import (
	"context"
	"fmt"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/config"
)

// QuickstartCurlProcedure is the authenticated Connect RPC procedure the
// printed quickstart curl calls. CompanyService.GetCompany is deliberate: it
// is NOT in server.DefaultPublicProcedures (so the call proves the bearer
// token is genuinely required and genuinely valid) but it also carries no
// entry in main.go's defaultProcedurePermissions map, so the RBAC
// interceptor lets any authenticated member of the seeded tenant through —
// not just an owner. Its only input is the seeded tenant's company id, which
// devseed.go already fixes at DevTenantCompanyID.
const QuickstartCurlProcedure = "/platform.v1.CompanyService/GetCompany"

// BuildQuickstartCurl renders the exact curl invocation a developer can copy
// and paste to call a real authenticated procedure against the running
// playground. Every value is a parameter — nothing here is a hand-typed
// literal that could silently drift from what the server actually issued;
// callers (printQuickstart below, and the test that proves this curl works)
// both build it from the same inputs.
func BuildQuickstartCurl(baseURL, token, companyID string) string {
	return fmt.Sprintf(
		`curl -s -X POST %s%s -H "Content-Type: application/json" -H "Authorization: Bearer %s" -d '{"id":"%s"}'`,
		baseURL, QuickstartCurlProcedure, token, companyID,
	)
}

// BuildQuickstartBanner renders the full startup quickstart block: the
// seeded identity, the base URL, the token (with a note that it is a local
// dev credential, per this TRD's constraint on not leaking it unlabeled into
// shared CI logs), a copy-pasteable curl that succeeds against a real
// authenticated procedure, and the dev-only/in-memory/data-loss notice this
// TRD requires verbatim. Kept as a pure string-building function (no server
// or auth dependency) so quickstart_test.go can assert its exact wording
// without standing up a server.
func BuildQuickstartBanner(baseURL, email, userID, companyID, token string) string {
	curl := BuildQuickstartCurl(baseURL, token, companyID)
	return fmt.Sprintf(`
======================= EDEN PLATFORM DEV PLAYGROUND =======================
  Identity    : %s  (user_id=%s)
  Company     : %s
  Base URL    : %s
  Token       : %s
                (a local dev credential — do not paste into shared CI logs)

  Try it now — this calls a real authenticated procedure and succeeds:

    %s

  DEV-ONLY. IN-MEMORY. ALL DATA IS LOST WHEN THIS PROCESS EXITS.
==============================================================================
`, email, userID, companyID, baseURL, token, curl)
}

// printQuickstart authenticates the seeded dev tenant user through the SAME
// real auth.Service.Login production path devsession.go's mintDevSession
// uses (see devsession.go) and prints the quickstart banner to stdout. It
// deliberately performs its own Login call rather than importing
// devsession.go's DevSession type: this TRD owns only quickstart.go,
// quickstart_test.go, and its stub — mintDevSession/DevSession belong to
// TRD 02's devsession.go, which this TRD does not modify. A second real
// Login call is inexpensive (in-memory devstore) and exercises the exact
// same production login path a second time, which only strengthens the
// "authenticate, don't fabricate" guarantee. A non-nil return is meant to be
// fatal in main.go, mirroring how other boot-time errors are already
// treated there.
func printQuickstart(cfg *config.PlatformConfig, authService *auth.Service) error {
	resp, err := authService.Login(context.Background(), DevTenantEmail, DevTenantPassword)
	if err != nil {
		return fmt.Errorf("quickstart: log in seeded dev tenant user: %w", err)
	}

	baseURL := "http://" + cfg.ServerAddr
	fmt.Println(BuildQuickstartBanner(baseURL, resp.User.Email, resp.User.ID.String(), DevTenantCompanyID.String(), resp.AccessToken))
	return nil
}
