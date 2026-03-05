package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var logger *slog.Logger

func main() {
	logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	start := time.Now()
	logger.Info("registration started")

	if err := run(); err != nil {
		logger.Error("registration failed", "error", err, "duration", time.Since(start))
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	logger.Info("registration completed", "duration", time.Since(start))
}

func run() error {
	stepStart := time.Now()
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: register <register-url> [repo]\n       register setup [repo]\n  repo: GitHub org/name to clone and build (default: housecat-inc/go-template)")
	}

	if os.Args[1] == "setup" {
		return runSetup()
	}

	logger.Info("parsing arguments")
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

	repoArg := "housecat-inc/go-template"
	if len(os.Args) >= 3 {
		repoArg = os.Args[2]
	}
	repo, branch := repoArg, "main"
	if at := strings.LastIndex(repoArg, "@"); at > 0 {
		repo = repoArg[:at]
		branch = repoArg[at+1:]
	}
	_, repoName, _ := strings.Cut(repo, "/")
	if repoName == "" {
		return fmt.Errorf("invalid repo %q: expected org/name[@branch]", repo)
	}

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("get hostname: %w", err)
	}
	appName, _, _ := strings.Cut(hostname, ".")
	logger.Info("parsed arguments", "app", appName, "repo", repo, "branch", branch, "duration", time.Since(stepStart))

	// Register client via RFC 7591
	stepStart = time.Now()
	clientID, clientSecret, scope, err := registerClient(token, endpoint, appName)
	if err != nil {
		return fmt.Errorf("register client: %w", err)
	}
	logger.Info("registered client", "client_id", clientID, "scope", scope, "duration", time.Since(stepStart))

	// Generate session secret
	stepStart = time.Now()
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return fmt.Errorf("generate session secret: %w", err)
	}
	sessionSecret := hex.EncodeToString(secretBytes)

	// Set up git proxy if git scope was granted
	stepStart = time.Now()
	if hasScope(scope, "git") {
		if err := setupGitProxy(issuer, clientID, clientSecret); err != nil {
			logger.Warn("git proxy setup failed", "error", err)
			fmt.Fprintf(os.Stderr, "WARNING: git proxy setup: %v\n", err)
		} else {
			logger.Info("git proxy configured", "duration", time.Since(stepStart))
		}
	} else {
		fmt.Println("==> No git scope granted, skipping git proxy setup")
		logger.Info("skipped git proxy", "reason", "no git scope")
	}

	// Clone and build the repo
	stepStart = time.Now()
	if err := cloneAndBuild(repo, repoName, branch); err != nil {
		return fmt.Errorf("clone and build: %w", err)
	}
	logger.Info("cloned and built", "repo", repo, "branch", branch, "duration", time.Since(stepStart))

	// Write .env
	stepStart = time.Now()
	if err := writeEnv(clientID, clientSecret, issuer, sessionSecret); err != nil {
		return fmt.Errorf("write env: %w", err)
	}
	logger.Info("wrote environment file", "duration", time.Since(stepStart))

	// Restart service
	stepStart = time.Now()
	fmt.Println("==> Restarting service...")
	if err := sudo("systemctl", "restart", "srv"); err != nil {
		return fmt.Errorf("restart service: %w", err)
	}
	logger.Info("restarted service", "duration", time.Since(stepStart))

	fmt.Println("==> Done")
	fmt.Printf("    App:       %s\n", appName)
	fmt.Printf("    Repo:      %s@%s\n", repo, branch)
	fmt.Printf("    Client ID: %s\n", clientID)
	_ = shell("systemctl", "status", "srv", "--no-pager")
	return nil
}

func runSetup() error {
	repoArg := "housecat-inc/go-template"
	if len(os.Args) >= 3 {
		repoArg = os.Args[2]
	}
	repo, branch := repoArg, "main"
	if at := strings.LastIndex(repoArg, "@"); at > 0 {
		repo = repoArg[:at]
		branch = repoArg[at+1:]
	}
	_, repoName, _ := strings.Cut(repo, "/")
	if repoName == "" {
		return fmt.Errorf("invalid repo %q: expected org/name[@branch]", repo)
	}

	stepStart := time.Now()
	if err := cloneAndBuild(repo, repoName, branch); err != nil {
		return fmt.Errorf("clone and build: %w", err)
	}
	logger.Info("cloned and built", "repo", repo, "branch", branch, "duration", time.Since(stepStart))

	stepStart = time.Now()
	fmt.Println("==> Restarting service...")
	if err := sudo("systemctl", "restart", "srv"); err != nil {
		return fmt.Errorf("restart service: %w", err)
	}
	logger.Info("restarted service", "duration", time.Since(stepStart))

	fmt.Println("==> Done")
	fmt.Printf("    Repo: %s@%s\n", repo, branch)
	_ = shell("systemctl", "status", "srv", "--no-pager")
	return nil
}

func cloneAndBuild(repo, repoName, branch string) error {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, repoName)

	if _, err := os.Stat(dir); err == nil {
		fmt.Printf("==> %s already exists, fetching...\n", dir)
		cmd := exec.Command("git", "fetch", "origin")
		cmd.Dir = dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git fetch: %w", err)
		}
		fmt.Printf("==> Checking out %s...\n", branch)
		checkout := exec.Command("git", "checkout", branch)
		checkout.Dir = dir
		checkout.Stdout = os.Stdout
		checkout.Stderr = os.Stderr
		if err := checkout.Run(); err != nil {
			return fmt.Errorf("git checkout %s: %w", branch, err)
		}
	} else {
		fmt.Printf("==> Cloning %s (shallow, branch %s)...\n", repo, branch)
		if err := shell("git", "clone", "--depth", "1", "--branch", branch,
			"https://github.com/"+repo+".git", dir); err != nil {
			return fmt.Errorf("git clone: %w", err)
		}
	}

	return serviceSetup(dir)
}

func serviceSetup(dir string) error {
	// Install tailwindcss if needed
	if _, err := exec.LookPath("tailwindcss"); err != nil {
		fmt.Println("==> Installing tailwindcss...")
		if err := shell("bash", "-c", "curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/download/v4.2.1/tailwindcss-linux-x64 && chmod +x tailwindcss-linux-x64 && sudo mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss"); err != nil {
			return fmt.Errorf("install tailwindcss: %w", err)
		}
	} else {
		fmt.Println("==> tailwindcss already installed")
	}

	// Download modules first so generators and build don't block on network
	fmt.Println("==> Downloading Go modules...")
	dl := exec.Command("go", "mod", "download")
	dl.Dir = dir
	dl.Stdout = os.Stdout
	dl.Stderr = os.Stderr
	if err := dl.Run(); err != nil {
		return fmt.Errorf("go mod download: %w", err)
	}

	// Run generators in parallel (templ, sqlc, tailwindcss are independent)
	fmt.Println("==> Running generators in parallel...")
	type generator struct {
		name string
		cmd  *exec.Cmd
	}
	gens := []generator{
		{"templ", dirCmd(filepath.Join(dir, "ui"), "go", "tool", "templ", "generate")},
		{"sqlc", dirCmd(filepath.Join(dir, "db"), "go", "tool", "github.com/sqlc-dev/sqlc/cmd/sqlc", "generate")},
		{"tailwindcss", dirCmd(filepath.Join(dir, "assets"), "tailwindcss", "-i", "css/input.css", "-o", "css/output.css", "--minify")},
	}

	var mu sync.Mutex
	var errs []error
	var wg sync.WaitGroup
	for _, g := range gens {
		wg.Add(1)
		go func(g generator) {
			defer wg.Done()
			start := time.Now()
			out, err := g.cmd.CombinedOutput()
			mu.Lock()
			defer mu.Unlock()
			if len(out) > 0 {
				fmt.Printf("    [%s] %s", g.name, out)
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", g.name, err))
			} else {
				fmt.Printf("    [%s] done (%s)\n", g.name, time.Since(start).Round(time.Millisecond))
			}
		}(g)
	}
	wg.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("generate failed: %v", errs)
	}

	// Build
	fmt.Println("==> Building...")
	build := dirCmd(dir, "go", "build", "-o", "bin/srv", "./cmd/srv")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}

	// Install binary
	fmt.Println("==> Installing binary...")
	for _, args := range [][]string{
		{"mkdir", "-p", "/opt/srv/bin", "/opt/srv/data"},
		{"rm", "-f", "/opt/srv/bin/srv"},
		{"cp", filepath.Join(dir, "bin/srv"), "/opt/srv/bin/srv"},
		{"chown", "root:root", "/opt/srv/bin/srv"},
		{"chmod", "0755", "/opt/srv/bin/srv"},
		{"chown", "-R", "exedev:exedev", "/opt/srv/data"},
		{"chmod", "0700", "/opt/srv/data"},
	} {
		if err := sudo(args...); err != nil {
			return fmt.Errorf("install: %w", err)
		}
	}

	// Copy .env if it exists
	home, _ := os.UserHomeDir()
	envFile := filepath.Join(home, ".env")
	if _, err := os.Stat(envFile); err == nil {
		_ = sudo("cp", envFile, "/opt/srv/data/.env")
		_ = sudo("chown", "exedev:exedev", "/opt/srv/data/.env")
		_ = sudo("chmod", "0600", "/opt/srv/data/.env")
	}

	// Install systemd unit
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

// dirCmd creates an exec.Cmd that runs in the given directory.
func dirCmd(dir, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd
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
		Scope:                   "openid email profile offline_access git",
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
	_ = shell("git", "config", "--global", "user.name", "shelley-agent[bot]")
	_ = shell("git", "config", "--global", "user.email", "2976885+shelley-agent[bot]@users.noreply.github.com")

	gobin := filepath.Join(os.Getenv("HOME"), "go", "bin")
	profileLines := fmt.Sprintf("\n# Housecat PATH\nexport PATH=%s:$PATH\n", gobin)

	// Write to both .profile and .bashrc for login and interactive shells
	for _, rcfile := range []string{".profile", ".bashrc"} {
		path := filepath.Join(os.Getenv("HOME"), rcfile)
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644); err == nil {
			_, _ = f.WriteString(profileLines)
			f.Close()
		}
	}

	fmt.Printf("    Proxy:       %s\n", proxyBaseClean)
	fmt.Printf("    insteadOf:   https://github.com/ -> %s/github.com/\n", proxyBaseClean)

	fmt.Println("==> Installing gh wrapper...")
	if err := shell("go", "install", "github.com/housecat-inc/go-template/cmd/gh@latest"); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: install gh wrapper: %v\n", err)
	}

	fmt.Println("==> Installing housecat CLI...")
	if err := shell("go", "install", "github.com/housecat-inc/go-template/cmd/housecat@latest"); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: install housecat CLI: %v\n", err)
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
