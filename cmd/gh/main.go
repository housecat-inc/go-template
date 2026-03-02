package main

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	realGH, err := findRealGH()
	if err != nil {
		return err
	}

	token, err := getToken()
	if err != nil {
		return err
	}

	login := exec.Command(realGH, "auth", "login", "--with-token")
	login.Stdin = strings.NewReader(token)
	login.Stderr = io.Discard
	if err := login.Run(); err != nil {
		return fmt.Errorf("gh auth login: %w", err)
	}

	if len(os.Args) > 1 {
		gh := exec.Command(realGH, os.Args[1:]...)
		gh.Stdin = os.Stdin
		gh.Stdout = os.Stdout
		gh.Stderr = os.Stderr
		if err := gh.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			return fmt.Errorf("gh: %w", err)
		}
	}

	return nil
}

func findRealGH() (string, error) {
	self, _ := os.Executable()
	self, _ = filepath.EvalSymlinks(self)

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		candidate := filepath.Join(dir, "gh")
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		if resolved == self {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find real gh binary in PATH")
}

func getToken() (string, error) {
	if proxyURL := os.Getenv("GH_PROXY_URL"); proxyURL != "" {
		return getTokenFromProxy(proxyURL)
	}
	return getTokenDirect()
}

func getTokenFromProxy(proxyURL string) (string, error) {
	proxyURL = strings.TrimRight(proxyURL, "/")
	req, err := http.NewRequest("GET", proxyURL+"/gh/token", nil)
	if err != nil {
		return "", fmt.Errorf("create proxy request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("proxy request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read proxy response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("proxy returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode proxy response: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("empty token from proxy")
	}
	return result.Token, nil
}

func getTokenDirect() (string, error) {
	appID := envOr("GH_APP_ID", "2976885")
	pemFile := envOr("GH_APP_PEM", filepath.Join(os.Getenv("HOME"), ".ssh", "shelley-agent.pem"))
	repo := os.Getenv("GH_APP_REPO")

	jwt, err := createJWT(appID, pemFile)
	if err != nil {
		return "", fmt.Errorf("create JWT: %w", err)
	}

	installationID, err := getInstallationID(jwt)
	if err != nil {
		return "", fmt.Errorf("get installation ID: %w", err)
	}

	token, err := getAccessToken(jwt, installationID, repo)
	if err != nil {
		return "", fmt.Errorf("get access token: %w", err)
	}

	return token, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func createJWT(appID, pemFile string) (string, error) {
	keyData, err := os.ReadFile(pemFile)
	if err != nil {
		return "", fmt.Errorf("read PEM file: %w", err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return "", fmt.Errorf("no PEM block found in %s", pemFile)
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		k, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return "", fmt.Errorf("parse private key: %w", err)
		}
		var ok bool
		key, ok = k.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("key is not RSA")
		}
	}

	now := time.Now().Unix()
	header := base64URLEncode([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := base64URLEncode([]byte(fmt.Sprintf(`{"iat":%d,"exp":%d,"iss":"%s"}`, now-60, now+600, appID)))

	signingInput := header + "." + payload
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	return signingInput + "." + base64URLEncode(sig), nil
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func getInstallationID(jwt string) (int64, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/app/installations", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var installations []struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&installations); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	if len(installations) == 0 {
		return 0, fmt.Errorf("no installations found")
	}
	return installations[0].ID, nil
}

func getAccessToken(jwt string, installationID int64, repo string) (string, error) {
	var body string
	if repo != "" {
		body = fmt.Sprintf(`{"repositories":["%s"]}`, repo)
	} else {
		body = "{}"
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("failed to get installation token")
	}
	return result.Token, nil
}
