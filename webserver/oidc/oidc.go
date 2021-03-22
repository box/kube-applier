// Package oidc implements the authentication flow for kube-applier. It is based
// on some assumptions: the authentication server supports the openid and email
// scopes, it exposes an introspection URL with which we can validate the
// id_token. User session is stored in an encrypted cookie and the encryption
// key is randomly generated in the package and cannot be provided, meaning that
// cookies are rendered invalid on a restart.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/gorilla/securecookie"
	"golang.org/x/oauth2"
)

const (
	userSessionCookieName = "session.kube-applier.io"
	// https://www.oauth.com/oauth2-servers/pkce/authorization-request/
	codeVerifierCharset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-._~"
)

var (
	codeVerifierCharsetLen = big.NewInt(int64(len(codeVerifierCharset)))
	codeVerifierLen        = 128
	secureCookie           *securecookie.SecureCookie

	// ErrRedirectRequired is returned by Authenticator.Authenticate if a
	// redirect has been written to the http.ResponseWriter. The handler should
	// respect this and return without writing anything else to the
	// ResponseWriter.
	ErrRedirectRequired = fmt.Errorf("redirect is required")
)

func init() {
	secureCookie = securecookie.New(
		securecookie.GenerateRandomKey(64),
		securecookie.GenerateRandomKey(32),
	)
	secureCookie.SetSerializer(securecookie.JSONEncoder{})
	secureCookie.MaxAge(0)
}

func randString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("could not generate random string: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type idTokenPayload struct {
	Email string `json:"email"`
	Iss   string `json:"iss"`
	Nonce string `json:"nonce"`
}

type userSession struct {
	CodeVerifier []byte `json:"code_verifier"`
	Domain       string `json:"-"`
	IDToken      string `json:"id_token"`
	Nonce        string `json:"nonce"`
	RedirectPath string `json:"redirect_path"`
	State        string `json:"state"`
}

func newUserSession(w http.ResponseWriter, r *http.Request) (*userSession, error) {
	session := &userSession{}
	state, err := randString(32)
	if err != nil {
		return nil, err
	}
	session.State = state
	nonce, err := randString(32)
	if err != nil {
		return nil, err
	}
	session.Nonce = nonce
	if err := session.newCodeVerifier(); err != nil {
		return nil, err
	}
	// By default, we will redirect back to the root path, or for a GET request
	// request we can simply redirect back to the path that was requested.
	// For all other types of requests we use the Referer header, if present,
	// since the oauth flow uses 303 redirects that revert to GET.
	session.RedirectPath = "/"
	if r.Method == http.MethodGet {
		session.RedirectPath = r.URL.Path
	} else if v := r.Referer(); v != "" {
		session.RedirectPath = v
	}
	return session, nil
}

func newUserSessionFromRequest(r *http.Request) (*userSession, error) {
	cookie, err := r.Cookie(userSessionCookieName)
	if err != nil {
		return nil, err
	}
	session := &userSession{}
	if err := secureCookie.Decode(userSessionCookieName, cookie.Value, &session); err != nil {
		return nil, err
	}
	return session, nil
}

// ParseIDToken parses the stored id token and returns the result.
func (u *userSession) ParseIDToken() (*idTokenPayload, error) {
	if u.IDToken == "" {
		return nil, fmt.Errorf("IDToken is empty")
	}
	parts := strings.Split(u.IDToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("cannot decode JWT payload: %w", err)
	}
	idt := &idTokenPayload{}
	if err := json.Unmarshal(payload, idt); err != nil {
		return nil, fmt.Errorf("cannot unmarshal JWT payload: %w", err)
	}
	return idt, nil
}

// Save writes the session to the response as a cookie.
func (u *userSession) Save(w http.ResponseWriter) error {
	cookieValue, err := secureCookie.Encode(userSessionCookieName, u)
	if err != nil {
		return err
	}
	// The cookie doesn't need to expire. If the token stored within expires, it
	// will trigger the auth flow. Additionally, SameSite lax mode is required
	// for the cookie to be sent on the callback request, see:
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Set-Cookie/SameSite#lax
	http.SetCookie(w, &http.Cookie{
		Name:     userSessionCookieName,
		Value:    cookieValue,
		Domain:   u.Domain,
		HttpOnly: true,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	})
	return nil
}

func (u *userSession) newCodeVerifier() error {
	cv := make([]byte, codeVerifierLen)
	for i := 0; i < codeVerifierLen; i++ {
		num, err := rand.Int(rand.Reader, codeVerifierCharsetLen)
		if err != nil {
			return err
		}
		cv[i] = codeVerifierCharset[num.Int64()]
	}
	u.CodeVerifier = cv
	return nil
}

func (u *userSession) codeChallengeOptions() []oauth2.AuthCodeOption {
	hash := sha256.Sum256(u.CodeVerifier)
	return []oauth2.AuthCodeOption{
		oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("code_challenge", base64.RawURLEncoding.EncodeToString(hash[:])),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("nonce", u.Nonce),
	}
}

type oidcIntrospectionResponse struct {
	Active   bool   `json:"active"`
	Exp      int64  `json:"exp"`
	Username string `json:"username"`
}

type oidcErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type oidcConfiguration struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	IntrospectionEndpoint string `json:"introspection_endpoint"`
}

// Authenticator implements the flow for authenticating using oidc.
type Authenticator struct {
	config           *oauth2.Config
	domain           string
	discoveredConfig oidcConfiguration
	httpClient       *http.Client
	issuer           url.URL
}

// NewAuthenticator returns a new Authenticator configured for a specific issuer
// using the provided values.
func NewAuthenticator(issuer, clientID, clientSecret, redirectURL string) (*Authenticator, error) {
	if issuer == "" {
		return nil, fmt.Errorf("issuer cannot be empty")
	}
	issuerURL, err := url.Parse(issuer)
	if err != nil {
		return nil, fmt.Errorf("cannot parse issuer url: %w", err)
	}
	if issuerURL.Scheme != "https" {
		return nil, fmt.Errorf("invalid issuer url, scheme must be https")
	}
	if issuerURL.Host == "" {
		return nil, fmt.Errorf("invalid issuer url, host is empty")
	}
	if clientID == "" {
		return nil, fmt.Errorf("client ID cannot be empty")
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("client secret cannot be empty")
	}
	if redirectURL == "" {
		return nil, fmt.Errorf("redirect URL cannot be empty")
	}
	parsedRedirectURL, err := url.Parse(redirectURL)
	if err != nil {
		return nil, fmt.Errorf("cannot parse redirect url: %w", err)
	}
	oa := &Authenticator{
		domain:     strings.Split(parsedRedirectURL.Host, ":")[0],
		httpClient: &http.Client{},
		issuer: url.URL{
			Scheme: issuerURL.Scheme,
			Host:   issuerURL.Host,
			Path:   issuerURL.Path,
		},
	}
	if err := oa.discoverConfiguration(); err != nil {
		return nil, err
	}
	oa.config = &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{"openid", "email"},
		RedirectURL:  redirectURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:  oa.discoveredConfig.AuthorizationEndpoint,
			TokenURL: oa.discoveredConfig.TokenEndpoint,
		},
	}
	return oa, nil
}

// Authenticate automates the process of retrieving the user's email address.
// It will detect and handle the oauth2 callback or otherwise try to validate
// an existing user session in order to do so. Ultimately, it will initiate a
// new authentication flow if required.
func (o *Authenticator) Authenticate(ctx context.Context, w http.ResponseWriter, r *http.Request) (string, error) {
	// this is handling authentication flow callbacks
	if r.FormValue("state") != "" {
		session, err := o.processCallback(ctx, w, r)
		if err != nil {
			return "", fmt.Errorf("invalid callback: %w", err)
		}
		redirectPath := ""
		if v := session.RedirectPath; v != "" {
			redirectPath = v
			session.RedirectPath = ""
		}
		session.Domain = o.domain
		if err := session.Save(w); err != nil {
			return "", fmt.Errorf("cannot save session: %w", err)
		}
		if redirectPath != "" {
			http.Redirect(w, r, redirectPath, http.StatusSeeOther)
			return "", ErrRedirectRequired
		}
		email, err := o.userEmail(ctx, session)
		if err != nil {
			return "", fmt.Errorf("cannot get user's email: %w", err)
		}
		return email, nil
	}
	// the user is already authenticated
	if email, err := o.UserEmail(ctx, r); err == nil {
		return email, nil
	}
	// we should initiate a new authentication flow
	session, err := newUserSession(w, r)
	if err != nil {
		return "", err
	}
	session.Domain = o.domain
	if err := session.Save(w); err != nil {
		return "", fmt.Errorf("cannot save session: %w", err)
	}
	http.Redirect(w, r, o.config.AuthCodeURL(session.State, session.codeChallengeOptions()...), http.StatusSeeOther)
	return "", ErrRedirectRequired
}

// UserEmail returns the email address of the user from their session, if they
// are already authenticated. It works similarly to Authenticate but does not
// handle oauth2 callbacks and will not initiate a new authentication flow.
func (o *Authenticator) UserEmail(ctx context.Context, r *http.Request) (string, error) {
	session, err := newUserSessionFromRequest(r)
	if err != nil {
		return "", err
	}
	email, err := o.userEmail(ctx, session)
	if err != nil {
		return "", fmt.Errorf("cannot get user's email: %w", err)
	}
	// We simply use the introspection endpoint to validate the token.
	// Although that's an extra HTTP call for each connection, it reduces
	// the code significantly when it comes to verifying the validity of the
	// IDToken. Alternatively, we can fetch the JWKS endpoint from discovery
	// and use the keys contained there to validate it locally.
	// eg.: https://developer.okta.com/docs/guides/validate-id-tokens/overview/
	ir, err := o.introspectToken(ctx, session.IDToken, "id_token")
	if err != nil {
		return "", err
	}
	if !ir.Active {
		return "", fmt.Errorf("session contains inactive id token")
	}
	return email, nil
}

// userEmail parses and validates the idToken stored in a userSession and
// returns the email address stored in it.
func (o *Authenticator) userEmail(ctx context.Context, session *userSession) (string, error) {
	idt, err := session.ParseIDToken()
	if err != nil {
		return "", fmt.Errorf("cannot parse id token: %w", err)
	}
	if idt.Nonce != session.Nonce {
		return "", fmt.Errorf("token nonce does not match state")
	}
	if idt.Iss != o.issuer.String() {
		return "", fmt.Errorf("invalid token issuer")
	}
	return idt.Email, nil
}

func (o *Authenticator) processCallback(ctx context.Context, w http.ResponseWriter, r *http.Request) (*userSession, error) {
	if r.FormValue("error") != "" {
		return nil, fmt.Errorf("error '%s': %s", r.FormValue("error"), r.FormValue("error_description"))
	}
	if r.FormValue("code") == "" {
		return nil, fmt.Errorf("missing code parameter")
	}
	if r.FormValue("state") == "" {
		return nil, fmt.Errorf("missing state parameter")
	}
	session, err := newUserSessionFromRequest(r)
	if err != nil {
		return nil, err
	}
	if session.State != r.FormValue("state") {
		return nil, fmt.Errorf("invalid state")
	}
	session.State = ""
	if len(session.CodeVerifier) == 0 {
		return nil, fmt.Errorf("cannot verify validity")
	}
	token, err := o.config.Exchange(ctx, r.FormValue("code"), oauth2.SetAuthURLParam("code_verifier", string(session.CodeVerifier)))
	if err != nil {
		return nil, err
	}
	session.CodeVerifier = nil
	idToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("invalid id_token type")
	}
	if idToken == "" {
		return nil, fmt.Errorf("token does not include id_token")
	}
	// we only want to keep the id_token:
	// - we can use the introspection endpoint to validate it
	// - it contains the email address of the user
	session.IDToken = idToken
	return session, nil
}

func (o *Authenticator) discoverConfiguration() error {
	discoveryURL := o.issuer
	discoveryURL.Path = path.Join(discoveryURL.Path, ".well-known/openid-configuration")
	req, err := http.NewRequest(http.MethodGet, discoveryURL.String(), nil)
	if err != nil {
		return err
	}
	return o.do(req, &o.discoveredConfig)
}

// introspectToken takes a token and token type and queries the introspection
// endpoint with it (https://tools.ietf.org/html/rfc7662#section-2.2)
func (o *Authenticator) introspectToken(ctx context.Context, token, tokenType string) (*oidcIntrospectionResponse, error) {
	data := url.Values{}
	data.Set("token", token)
	data.Set("token_type_hint", tokenType)
	data.Set("client_id", o.config.ClientID)
	data.Set("client_secret", o.config.ClientSecret)
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		o.discoveredConfig.IntrospectionEndpoint,
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := &oidcIntrospectionResponse{}
	if err := o.do(req, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (o *Authenticator) do(req *http.Request, data interface{}) error {
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		e := oidcErrorResponse{}
		if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
			return fmt.Errorf("request failed with status %d and unknown error", resp.StatusCode)
		}
		return fmt.Errorf("request failed with status %d and error '%s': %s", resp.StatusCode, e.Error, e.ErrorDescription)
	}
	err = json.NewDecoder(resp.Body).Decode(data)
	if err != nil {
		return err
	}
	return nil
}
