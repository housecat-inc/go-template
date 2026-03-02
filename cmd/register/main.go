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
		return fmt.Errorf("usage: register <register-url> [repo]\n  repo: GitHub org/name to clone and build (default: housecat-inc/go-template)")
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

	repo := "housecat-inc/go-template"
	if len(os.Args) >= 3 {
		repo = os.Args[2]
	}
	_, repoName, _ := strings.Cut(repo, "/")
	if repoName == "" {
		return fmt.Errorf("invalid repo %q: expected org/name", repo)
	}

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("get hostname: %w", err)
	}
	appName, _, _ := strings.Cut(hostname, ".")

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

	// Clone and build the repo
	if err := cloneAndBuild(repo, repoName); err != nil {
		return fmt.Errorf("clone and build: %w", err)
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
	fmt.Printf("    Repo:      %s\n", repo)
	fmt.Printf("    Client ID: %s\n", clientID)
	_ = shell("systemctl", "status", "srv", "--no-pager")
	return nil
}

func cloneAndBuild(repo, repoName string) error {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, repoName)

	if _, err := os.Stat(dir); err == nil {
		fmt.Printf("==> %s already exists, pulling...\n", dir)
		cmd := exec.Command("git", "pull", "origin", "main")
		cmd.Dir = dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git pull: %w", err)
		}
	} else {
		fmt.Printf("==> Cloning %s...\n", repo)
		if err := shell("git", "clone", "-b", "main", "https://github.com/"+repo+".git", dir); err != nil {
			return fmt.Errorf("git clone: %w", err)
		}
	}

	return serviceSetup(dir)
}

func serviceSetup(dir string) error {
	if _, err := exec.LookPath("tailwindcss"); err != nil {
		fmt.Println("==> Installing tailwindcss...")
		if err := shell("bash", "-c", "curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/download/v4.2.1/tailwindcss-linux-x64 && chmod +x tailwindcss-linux-x64 && sudo mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss"); err != nil {
			return fmt.Errorf("install tailwindcss: %w", err)
		}
	} else {
		fmt.Println("==> tailwindcss already installed")
	}

	fmt.Printf("==> Running make install in %s...\n", dir)
	cmd := exec.Command("make", "install")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("make install: %w", err)
	}

	fmt.Println("==> Installing systemd unit...")
	svcFile := filepath.Join(dir, "srv.service")
	if err := sudo("cp", svcFile, "/etc/systemd/system/srv.service"); err != nil {
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

type clientMetadata struct {
	ClientName              string   `json:"client_name"`
	GrantTypes              []string `json:"grant_types"`
	RedirectURIs            []string `json:"redirect_uris"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
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

	resp, err := http.Get(issuer + "/gitproxy/ca.crt")
	if err != nil {
		return fmt.Errorf("probe git proxy: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		fmt.Println("    Git proxy not enabled on auth server, skipping")
		return nil
	}

	issuerURL, err := url.Parse(issuer)
	if err != nil {
		return fmt.Errorf("parse issuer: %w", err)
	}
	proxyHost := issuerURL.Host

	proxyBase := "https://" + url.UserPassword(clientID, clientSecret).String() + "@" + proxyHost
	proxyBaseClean := "https://" + proxyHost

	_ = shell("git", "config", "--global", "url."+proxyBase+"/github.com/.insteadOf", "https://github.com/")

	ghProxyURL := "https://" + url.UserPassword(clientID, clientSecret).String() + "@" + proxyHost
	profileLines := fmt.Sprintf("\n# Housecat git proxy\nexport GH_PROXY_URL=%s\n", ghProxyURL)

	bashrc := filepath.Join(os.Getenv("HOME"), ".bashrc")
	if f, err := os.OpenFile(bashrc, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644); err == nil {
		_, _ = f.WriteString(profileLines)
		f.Close()
	}

	fmt.Printf("    Proxy:       %s\n", proxyBaseClean)
	fmt.Printf("    insteadOf:   https://github.com/ -> %s/github.com/\n", proxyBaseClean)

	fmt.Println("==> Installing gh wrapper...")
	if err := shell("go", "install", "github.com/housecat-inc/go-template/cmd/gh@latest"); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: install gh wrapper: %v\n", err)
	}

	fmt.Println("==> Smoke testing git proxy...")
	out, err := exec.Command("git", "ls-remote", "--heads", "https://github.com/housecat-inc/go-template.git").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git ls-remote failed: %w\n%s", err, out)
	}
	lines := strings.Count(strings.TrimSpace(string(out)), "\n") + 1
	fmt.Printf("    git ls-remote: OK (%d refs)\n", lines)

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
