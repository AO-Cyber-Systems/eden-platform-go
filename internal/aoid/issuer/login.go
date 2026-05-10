package issuer

import (
	_ "embed"
	"html/template"
	"net/http"

	"github.com/aocybersystems/eden-platform-go/internal/aoid/clients"
)

//go:embed login.tmpl.html
var loginTmplSrc string

var loginTmpl = template.Must(template.New("login").Parse(loginTmplSrc))

// loginPageData is the model for login.tmpl.html.
type loginPageData struct {
	ClientName          string
	Issuer              string
	Email               string
	Error               string
	ResponseType        string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	Nonce               string
	CodeChallenge       string
	CodeChallengeMethod string
	Prompt              string
}

// renderLogin executes the login template with the request's auth params
// preserved as hidden form fields so the POST submit returns to /authorize
// with all OIDC context intact.
func (i *Issuer) renderLogin(w http.ResponseWriter, r *http.Request, ar authorizeRequest, client *clients.Client, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	data := loginPageData{
		ClientName:          client.Name,
		Issuer:              i.Config.Issuer,
		Email:               r.FormValue("email"),
		Error:               errMsg,
		ResponseType:        ar.ResponseType,
		ClientID:            ar.ClientID,
		RedirectURI:         ar.RedirectURI,
		Scope:               ar.Scope,
		State:               ar.State,
		Nonce:               ar.Nonce,
		CodeChallenge:       ar.CodeChallenge,
		CodeChallengeMethod: ar.CodeChallengeMethod,
		Prompt:              ar.Prompt,
	}
	_ = loginTmpl.Execute(w, data)
}
