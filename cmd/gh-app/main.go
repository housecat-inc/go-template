package main

import (
	"crypto"
	"crypto/rand"
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	appID := envOr("GH_APP_ID", "2976885")
	pemFile := envOr("GH_APP_PEM", filepath.Join(os.Getenv("HOME"), ".ssh", "shelley-agent.pem"))
	repo := os.Getenv("GH_APP_REPO")

	now := time.Now()
	jwt, err := createJWT(appID, pemFile, now)
	if err != nil {
		return fmt.Errorf("creating JWT: %w", err)
	}

	installationID, err := getInstallationID(jwt)
	if err != nil {
		return fmt.Errorf("getting installation ID: %w", err)
	}

	token, err := getAccessToken(jwt, installationID, repo)
	if err != nil {
		return fmt.Errorf("getting access token: %w", err)
	}

	loginCmd := exec.Command("gh", "auth", "login", "--with-token")
	loginCmd.Stdin = strings.NewReader(token)
	loginCmd.Stderr = io.Discard
	if err := loginCmd.Run(); err != nil {
		return fmt.Errorf("gh auth login: %w", err)
	}

	if len(os.Args) > 1 {
		ghCmd := exec.Command("gh", os.Args[1:]...)
		ghCmd.Stdin = os.Stdin
		ghCmd.Stdout = os.Stdout
		ghCmd.Stderr = os.Stderr
		return ghCmd.Run()
	}

	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func createJWT(appID, pemFile string, now time.Time) (string, error) {
	keyData, err := os.ReadFile(pemFile)
	if err != nil {
		return "", fmt.Errorf("reading PEM file: %w", err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		k, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return "", fmt.Errorf("parsing private key: %w", err)
		}
		var ok bool
		key, ok = k.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("private key is not RSA")
		}
	}

	header := base64URLEncode([]byte(`{"alg":"RS256","typ":"JWT"}`))

	iat := now.Add(-60 * time.Second).Unix()
	exp := now.Add(10 * time.Minute).Unix()
	payload := base64URLEncode([]byte(fmt.Sprintf(`{"iat":%d,"exp":%d,"iss":"%s"}`, iat, exp, appID)))

	signingInput := header + "." + payload
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("signing JWT: %w", err)
	}

	return signingInput + "." + base64URLEncode(sig), nil
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

type installation struct {
	ID int64 `json:"id"`
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

	var installations []installation
	if err := json.NewDecoder(resp.Body).Decode(&installations); err != nil {
		return 0, fmt.Errorf("decoding installations: %w", err)
	}

	if len(installations) == 0 {
		return 0, fmt.Errorf("no installations found")
	}

	return installations[0].ID, nil
}

func getAccessToken(jwt string, installationID int64, repo string) (string, error) {
	body := "{}"
	if repo != "" {
		body = fmt.Sprintf(`{"repositories":["%s"]}`, repo)
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
		return "", fmt.Errorf("decoding token response: %w", err)
	}

	if result.Token == "" {
		return "", fmt.Errorf("failed to get installation token")
	}

	return result.Token, nil
}
