// Package oauth fetches user info from OAuth 2.0 providers (Google, GitHub,
// Facebook, Twitter, Apple, Microsoft) using a previously-obtained access
// token.
//
// It is the platform-side promotion of aodex-go/internal/auth/oauth.go.
// Provider endpoints are package-level variables so tests can rebind them
// to httptest servers without needing a custom http.RoundTripper.
package oauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// UserInfo is the normalized user info returned by every supported provider.
type UserInfo struct {
	UID   string
	Email string
	Name  string
	Image string
}

// Endpoints are the URLs called for each provider. Tests may override these
// with httptest server URLs.
var Endpoints = struct {
	Google         string
	GitHubUser     string
	GitHubEmails   string
	Facebook       string
	Twitter        string
	MicrosoftGraph string
}{
	Google:         "https://www.googleapis.com/oauth2/v3/userinfo",
	GitHubUser:     "https://api.github.com/user",
	GitHubEmails:   "https://api.github.com/user/emails",
	Facebook:       "https://graph.facebook.com/me",
	Twitter:        "https://api.twitter.com/2/users/me",
	MicrosoftGraph: "https://graph.microsoft.com/v1.0/me",
}

// FetchUserInfo validates an OAuth access token with the given provider and
// returns the normalized user info. Supported providers: "google", "github",
// "facebook", "twitter", "apple", "microsoft".
func FetchUserInfo(provider, accessToken string) (*UserInfo, error) {
	return FetchUserInfoWithClient(provider, accessToken, defaultHTTPClient())
}

// FetchUserInfoWithClient is the testable variant of FetchUserInfo.
func FetchUserInfoWithClient(provider, accessToken string, client *http.Client) (*UserInfo, error) {
	switch provider {
	case "google":
		return fetchGoogle(accessToken, client)
	case "github":
		return fetchGitHub(accessToken, client)
	case "facebook":
		return fetchFacebook(accessToken, client)
	case "twitter":
		return fetchTwitter(accessToken, client)
	case "apple":
		return fetchApple(accessToken)
	case "microsoft":
		return fetchMicrosoft(accessToken, client)
	default:
		return nil, fmt.Errorf("unsupported OAuth provider: %s", provider)
	}
}

func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

func fetchGoogle(accessToken string, client *http.Client) (*UserInfo, error) {
	req, err := http.NewRequest(http.MethodGet, Endpoints.Google, nil)
	if err != nil {
		return nil, fmt.Errorf("creating google request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling google userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("google userinfo returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding google response: %w", err)
	}

	return &UserInfo{
		UID:   result.Sub,
		Email: result.Email,
		Name:  result.Name,
		Image: result.Picture,
	}, nil
}

func fetchGitHub(accessToken string, client *http.Client) (*UserInfo, error) {
	req, err := http.NewRequest(http.MethodGet, Endpoints.GitHubUser, nil)
	if err != nil {
		return nil, fmt.Errorf("creating github request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling github user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github user returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding github response: %w", err)
	}

	email := result.Email
	if email == "" {
		var fetchErr error
		email, fetchErr = fetchGitHubPrimaryEmail(accessToken, client)
		if fetchErr != nil {
			return nil, fmt.Errorf("fetching github primary email: %w", fetchErr)
		}
	}

	name := result.Name
	if name == "" {
		name = result.Login
	}

	return &UserInfo{
		UID:   strconv.FormatInt(result.ID, 10),
		Email: email,
		Name:  name,
		Image: result.AvatarURL,
	}, nil
}

func fetchGitHubPrimaryEmail(accessToken string, client *http.Client) (string, error) {
	req, err := http.NewRequest(http.MethodGet, Endpoints.GitHubEmails, nil)
	if err != nil {
		return "", fmt.Errorf("creating github emails request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling github emails: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github emails returned %d", resp.StatusCode)
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", fmt.Errorf("decoding github emails: %w", err)
	}

	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}

	return "", fmt.Errorf("no primary verified email found")
}

func fetchFacebook(accessToken string, client *http.Client) (*UserInfo, error) {
	url := Endpoints.Facebook + "?fields=id,name,email&access_token=" + accessToken
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating facebook request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling facebook graph: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("facebook graph returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding facebook response: %w", err)
	}

	return &UserInfo{
		UID:   result.ID,
		Email: result.Email,
		Name:  result.Name,
	}, nil
}

func fetchTwitter(accessToken string, client *http.Client) (*UserInfo, error) {
	req, err := http.NewRequest(http.MethodGet, Endpoints.Twitter+"?user.fields=id,name,username", nil)
	if err != nil {
		return nil, fmt.Errorf("creating twitter request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling twitter users/me: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("twitter users/me returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Username string `json:"username"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding twitter response: %w", err)
	}

	return &UserInfo{
		UID:  result.Data.ID,
		Name: result.Data.Name,
	}, nil
}

// fetchApple parses the Apple id_token JWT to extract user info. Apple does
// not have a userinfo endpoint — the access_token IS the id_token (JWT).
func fetchApple(accessToken string) (*UserInfo, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid apple id_token: expected JWT format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decoding apple jwt payload: %w", err)
	}

	var claims struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("parsing apple jwt claims: %w", err)
	}

	if claims.Sub == "" {
		return nil, fmt.Errorf("apple id_token missing sub claim")
	}

	return &UserInfo{
		UID:   claims.Sub,
		Email: claims.Email,
	}, nil
}

func fetchMicrosoft(accessToken string, client *http.Client) (*UserInfo, error) {
	req, err := http.NewRequest(http.MethodGet, Endpoints.MicrosoftGraph, nil)
	if err != nil {
		return nil, fmt.Errorf("creating microsoft request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling microsoft graph: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("microsoft graph returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID                string `json:"id"`
		DisplayName       string `json:"displayName"`
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding microsoft response: %w", err)
	}

	email := result.Mail
	if email == "" {
		email = result.UserPrincipalName
	}

	return &UserInfo{
		UID:   result.ID,
		Email: email,
		Name:  result.DisplayName,
	}, nil
}
