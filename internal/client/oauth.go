package client

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/taozhou/gleand/internal/config"
)

const softwareID = "gleand"

type OAuthClient struct {
	baseURL string
	logger  *slog.Logger
}

type registrationResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

type TokenData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
}

func NewOAuthClient(baseURL string, logger *slog.Logger) *OAuthClient {
	return &OAuthClient{baseURL: baseURL, logger: logger}
}

func (o *OAuthClient) registerClient(redirectURI string) (*registrationResponse, error) {
	regURL := strings.TrimRight(o.baseURL, "/") + "/oauth/register"
	reqBody := map[string]any{
		"client_name":                "gleand CLI",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"scope":                      "openid offline_access search chat",
		"software_id":                softwareID,
		"software_version":           config.Version,
	}

	body, _ := json.Marshal(reqBody)
	resp, err := http.Post(regURL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("registration request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registration failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var reg registrationResponse
	if err := json.Unmarshal(respBody, &reg); err != nil {
		return nil, fmt.Errorf("parsing registration response: %w", err)
	}
	return &reg, nil
}

func (o *OAuthClient) Authorize(ctx context.Context) (*TokenData, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("starting local server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	fmt.Println("Registering OAuth client...")
	reg, err := o.registerClient(redirectURI)
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("dynamic client registration failed: %w", err)
	}

	verifier, challenge := generatePKCE()
	state := generateRandomString(32)

	authURL := fmt.Sprintf("%s/oauth/authorize?%s",
		strings.TrimRight(o.baseURL, "/"),
		url.Values{
			"response_type":         {"code"},
			"client_id":             {reg.ClientID},
			"redirect_uri":          {redirectURI},
			"state":                 {state},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
			"scope":                 {"openid offline_access search chat"},
		}.Encode(),
	)

	fmt.Printf("Opening browser for authentication...\n")
	fmt.Printf("If the browser doesn't open, visit:\n%s\n\n", authURL)
	openBrowser(authURL)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "Invalid state", http.StatusBadRequest)
			errCh <- fmt.Errorf("OAuth state mismatch")
			return
		}
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			desc := r.URL.Query().Get("error_description")
			http.Error(w, "Authentication failed: "+desc, http.StatusBadRequest)
			errCh <- fmt.Errorf("OAuth error: %s — %s", errMsg, desc)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "No code received", http.StatusBadRequest)
			errCh <- fmt.Errorf("no authorization code in callback")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><h2>Authentication successful!</h2><p>You can close this tab and return to gleand.</p><script>window.close()</script></body></html>`)
		codeCh <- code
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer server.Close()

	select {
	case code := <-codeCh:
		token, err := o.exchangeCode(reg.ClientID, code, redirectURI, verifier)
		if err != nil {
			return nil, err
		}
		token.ClientID = reg.ClientID
		return token, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authentication timed out after 5 minutes")
	}
}

func (o *OAuthClient) RefreshAccessToken(clientID, refreshToken string) (*TokenData, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	}

	token, err := o.tokenRequest(data)
	if err != nil {
		return nil, err
	}
	token.ClientID = clientID
	return token, nil
}

func (o *OAuthClient) exchangeCode(clientID, code, redirectURI, verifier string) (*TokenData, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}

	return o.tokenRequest(data)
}

func (o *OAuthClient) tokenRequest(data url.Values) (*TokenData, error) {
	tokenURL := strings.TrimRight(o.baseURL, "/") + "/oauth/token"

	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var token TokenData
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}

	if token.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Unix() + int64(token.ExpiresIn)
	}

	return &token, nil
}

func TokenStorePath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(configDir, "gleand", "token.json")
}

func SaveToken(token *TokenData) error {
	path := TokenStorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func LoadToken() (*TokenData, error) {
	data, err := os.ReadFile(TokenStorePath())
	if err != nil {
		return nil, err
	}
	var token TokenData
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func generatePKCE() (verifier, challenge string) {
	verifier = generateRandomString(64)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

func generateRandomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		cmd.Start()
	}
}
