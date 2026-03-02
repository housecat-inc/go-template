package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
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
		return fmt.Errorf("usage: app-setup <token>")
	}
	token := os.Args[1]

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("get hostname: %w", err)
	}
	appName, _, _ := strings.Cut(hostname, ".")

	// Service setup
	if err := serviceSetup(); err != nil {
		return fmt.Errorf("service setup: %w", err)
	}

	// App registration
	clientID, clientSecret, err := registerApp(token, appName)
	if err != nil {
		return fmt.Errorf("register app: %w", err)
	}

	// Generate session secret
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return fmt.Errorf("generate session secret: %w", err)
	}
	sessionSecret := hex.EncodeToString(secretBytes)

	// Write .env
	if err := writeEnv(clientID, clientSecret, sessionSecret); err != nil {
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

type setupRequest struct {
	CallbackURLs []string `json:"callback_urls"`
	Name         string   `json:"name"`
	Scopes       []string `json:"scopes"`
	Token        string   `json:"token"`
}

type setupResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func registerApp(token, appName string) (string, string, error) {
	fmt.Printf("==> Registering app '%s' with Housecat Auth...\n", appName)

	body, err := json.Marshal(setupRequest{
		CallbackURLs: []string{
			fmt.Sprintf("https://%s.exe.xyz/auth/callback", appName),
			fmt.Sprintf("https://%s.exe.xyz:8000/auth/callback", appName),
		},
		Name:   appName,
		Scopes: []string{"login", "github"},
		Token:  token,
	})
	if err != nil {
		return "", "", fmt.Errorf("marshal request: %w", err)
	}

	resp, err := http.Post("https://hc-auth-dev.exe.xyz/admin/apps/setup", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("POST setup: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("setup returned %d: %s", resp.StatusCode, respBody)
	}

	var result setupResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", fmt.Errorf("decode response: %w", err)
	}
	if result.ClientID == "" {
		return "", "", fmt.Errorf("empty client_id in response: %s", respBody)
	}

	return result.ClientID, result.ClientSecret, nil
}

func writeEnv(clientID, clientSecret, sessionSecret string) error {
	fmt.Println("==> Writing /opt/srv/data/.env...")

	content := fmt.Sprintf("HOUSECAT_CLIENT_ID=%s\nHOUSECAT_CLIENT_SECRET=%s\nOAUTH_ISSUER=https://hc-auth-dev.exe.xyz\nSESSION_SECRET=%s\n",
		clientID, clientSecret, sessionSecret)

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

func shell(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func sudo(args ...string) error {
	return shell("sudo", args...)
}
