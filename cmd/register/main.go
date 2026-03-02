package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: register https://$TOKEN@hostname/register")
	}

	u, err := url.Parse(os.Args[1])
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	token := u.User.Username()
	if token == "" {
		return fmt.Errorf("token missing from URL userinfo")
	}
	endpoint := fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path)
	issuer := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("get hostname: %w", err)
	}
	appName, _, _ := strings.Cut(hostname, ".")

	// Service setup
	if err := serviceSetup(); err != nil {
		return fmt.Errorf("service setup: %w", err)
	}

	// Register client via RFC 7591
	clientID, clientSecret, scope, err := registerClient(token, endpoint, appName)
	if err != nil {
		return fmt.Errorf("register client: %w", err)
	}

	// Generate session secret
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return fmt.Errorf("generate session secret: %w", err)
	}
	sessionSecret := hex.EncodeToString(secretBytes)

	// Set up git proxy if git scope was granted
	if hasScope(scope, "git") {
		if err := setupGitProxy(issuer, clientID, clientSecret); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: git proxy setup: %v\n", err)
		}
	} else {
		fmt.Println("==> No git scope granted, skipping git proxy setup")
	}

	// Write .env
	if err := writeEnv(clientID, clientSecret, issuer, sessionSecret); err != nil {
		return fmt.Errorf("write env: %w", err)
	}

	// Restart service
	fmt.Println("==> Restarting service...")
	if err := sudo("systemctl", "restart", "srv"); err != nil {
		return fmt.Errorf("restart service: %w", err)
	}

	fmt.Println("==> Done")
	fmt.Printf("    App:       %s\n", appName)
	fmt.Printf("    Client ID: %s\n", clientID)
	_ = shell("systemctl", "status", "srv", "--no-pager")
	return nil
}

func serviceSetup() error {
	// Install tailwindcss
	if _, err := exec.LookPath("tailwindcss"); err != nil {
		fmt.Println("==> Installing tailwindcss...")
		if err := shell("bash", "-c", "curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/download/v4.2.1/tailwindcss-linux-x64 && chmod +x tailwindcss-linux-x64 && sudo mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss"); err != nil {
			return fmt.Errorf("install tailwindcss: %w", err)
		}
	} else {
		fmt.Println("==> tailwindcss already installed")
	}

	// Build and install
	fmt.Println("==> Running make install...")
	if err := shell("make", "install"); err != nil {
		return fmt.Errorf("make install: %w", err)
	}

	// Install systemd unit
	fmt.Println("==> Installing systemd unit...")
	if err := sudo("cp", "srv.service", "/etc/systemd/system/srv.service"); err != nil {
		return fmt.Errorf("copy service file: %w", err)
	}
	if err := sudo("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if err := sudo("systemctl", "enable", "srv.service"); err != nil {
		return fmt.Errorf("enable service: %w", err)
	}

	return nil
}

// RFC 7591 OAuth 2.0 Dynamic Client Registration request.
type clientMetadata struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

type clientRegistrationResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Scope        string `json:"scope"`
}

func registerClient(token, endpoint, appName string) (string, string, string, error) {
	fmt.Printf("==> Registering client '%s' with %s...\n", appName, endpoint)

	body, err := json.Marshal(clientMetadata{
		ClientName: appName,
		RedirectURIs: []string{
			fmt.Sprintf("https://%s.exe.xyz/auth/callback", appName),
			fmt.Sprintf("https://%s.exe.xyz:8000/auth/callback", appName),
		},
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "client_secret_basic",
		Scope:                   "openid email profile git",
	})
	if err != nil {
		return "", "", "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("POST register: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("register returned %d: %s", resp.StatusCode, respBody)
	}

	var result clientRegistrationResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", "", fmt.Errorf("decode response: %w", err)
	}
	if result.ClientID == "" {
		return "", "", "", fmt.Errorf("empty client_id in response: %s", respBody)
	}

	return result.ClientID, result.ClientSecret, result.Scope, nil
}

func writeEnv(clientID, clientSecret, issuer, sessionSecret string) error {
	fmt.Println("==> Writing /opt/srv/data/.env...")

	content := fmt.Sprintf("HOUSECAT_CLIENT_ID=%s\nHOUSECAT_CLIENT_SECRET=%s\nOAUTH_ISSUER=%s\nSESSION_SECRET=%s\n",
		clientID, clientSecret, issuer, sessionSecret)

	f, err := os.CreateTemp("", "env-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	f.Close()

	if err := sudo("mkdir", "-p", "/opt/srv/data"); err != nil {
		return err
	}
	if err := sudo("cp", f.Name(), "/opt/srv/data/.env"); err != nil {
		return err
	}
	if err := sudo("chown", "exedev:exedev", "/opt/srv/data/.env"); err != nil {
		return err
	}
	if err := sudo("chmod", "0600", "/opt/srv/data/.env"); err != nil {
		return err
	}

	return nil
}

func setupGitProxy(issuer, clientID, clientSecret string) error {
	fmt.Println("==> Setting up git proxy...")

	caURL := issuer + "/gitproxy/ca.crt"
	resp, err := http.Get(caURL)
	if err != nil {
		return fmt.Errorf("fetch ca: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		fmt.Println("    Git proxy not enabled on auth server, skipping")
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch ca: status %d", resp.StatusCode)
	}

	caCert, _ := io.ReadAll(resp.Body)

	configDir := filepath.Join(os.Getenv("HOME"), ".config", "gitproxy")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	caCertPath := filepath.Join(configDir, "ca.crt")
	if err := os.WriteFile(caCertPath, caCert, 0644); err != nil {
		return fmt.Errorf("write ca cert: %w", err)
	}

	// Combined CA bundle (system CAs + git proxy CA) for Go programs
	systemCA, _ := os.ReadFile("/etc/ssl/certs/ca-certificates.crt")
	combinedPath := filepath.Join(configDir, "combined-ca.crt")
	if err := os.WriteFile(combinedPath, append(systemCA, caCert...), 0644); err != nil {
		return fmt.Errorf("write combined ca: %w", err)
	}

	// Derive proxy address from issuer hostname
	proxyHost := strings.TrimPrefix(issuer, "https://")
	proxyHost = strings.TrimPrefix(proxyHost, "http://")
	if idx := strings.Index(proxyHost, ":"); idx != -1 {
		proxyHost = proxyHost[:idx]
	}
	proxyAddr := "http://" + url.UserPassword(clientID, clientSecret).String() + "@" + proxyHost + ":8443"
	proxyAddrClean := "http://" + proxyHost + ":8443"

	// Configure git to use proxy for github.com
	gitConfigs := [][2]string{
		{"http.https://github.com/.proxy", proxyAddrClean},
		{"http.https://github.com/.sslCAInfo", caCertPath},
		{"http.https://api.github.com/.proxy", proxyAddrClean},
		{"http.https://api.github.com/.sslCAInfo", caCertPath},
	}
	for _, cfg := range gitConfigs {
		_ = shell("git", "config", "--global", cfg[0], cfg[1])
	}

	// Add proxy env vars to .bashrc
	profileLines := fmt.Sprintf("\n# Housecat git proxy\nexport HTTPS_PROXY=%s\nexport SSL_CERT_FILE=%s\nexport GH_TOKEN=gitproxy\n",
		proxyAddr, combinedPath)

	// Also write credentials to a file for git config
	proxyAuthPath := filepath.Join(configDir, "proxy-auth")
	if err := os.WriteFile(proxyAuthPath, []byte(proxyAddr), 0600); err != nil {
		return fmt.Errorf("write proxy auth: %w", err)
	}

	bashrc := filepath.Join(os.Getenv("HOME"), ".bashrc")
	if f, err := os.OpenFile(bashrc, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644); err == nil {
		_, _ = f.WriteString(profileLines)
		f.Close()
	}

	fmt.Printf("    CA cert:     %s\n", caCertPath)
	fmt.Printf("    Proxy:       %s\n", proxyAddrClean)
	fmt.Printf("    GH_TOKEN:    gitproxy (proxy injects real credentials)\n")

	return nil
}

func hasScope(scopes, target string) bool {
	for _, s := range strings.Fields(scopes) {
		if s == target {
			return true
		}
	}
	return false
}

func shell(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func sudo(args ...string) error {
	return shell("sudo", args...)
}
