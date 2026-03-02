package exedev

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"golang.org/x/crypto/ssh"
)

type Client struct {
	BaseURL    string
	httpClient *http.Client
	Signer     ssh.Signer
}

// VM represents a virtual machine returned by "ls --json".
type VM struct {
	HTTPSURL      string `json:"https_url"`
	Image         string `json:"image"`
	Name          string `json:"vm_name"`
	Region        string `json:"region"`
	RegionDisplay string `json:"region_display"`
	ShareStatus   string `json:"-"`
	ShelleyURL    string `json:"shelley_url"`
	SSHDest       string `json:"ssh_dest"`
	Status        string `json:"status"`
}

// New creates a Client by loading an ed25519 private key from disk.
func New(keyPath string) (*Client, error) {
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, errors.Wrap(err, "read key file")
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, errors.Wrap(err, "parse private key")
	}
	return &Client{
		BaseURL:    "https://exe.dev",
		httpClient: &http.Client{Timeout: 30 * time.Second},
		Signer:     signer,
	}, nil
}

// Exec runs a command via POST /exec with a short-lived exe0 token.
// The token permission is derived from the first word of the command.
func (c *Client) Exec(ctx context.Context, command string) (string, error) {
	return c.ExecWithPerm(ctx, command, extractCmdName(command))
}

// ExecWithPerm runs a command with an explicit token permission string.
func (c *Client) ExecWithPerm(ctx context.Context, command, perm string) (string, error) {
	token, err := SignToken(c.Signer, []string{perm}, 5*time.Minute)
	if err != nil {
		return "", errors.Wrap(err, "sign token")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/exec",
		strings.NewReader(command))
	if err != nil {
		return "", errors.Wrap(err, "create request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "text/plain")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "exec request")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "read response")
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.Newf("exec %q failed: %d: %s", command, resp.StatusCode, string(body))
	}
	return string(body), nil
}

// ListVMs calls "ls --json" and parses the response.
func (c *Client) ListVMs(ctx context.Context) ([]VM, error) {
	out, err := c.Exec(ctx, "ls --json")
	if err != nil {
		return nil, errors.Wrap(err, "list vms")
	}
	var resp struct {
		VMs []VM `json:"vms"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, errors.Wrap(err, "parse vms json")
	}
	return resp.VMs, nil
}

// ShareInfo represents the sharing status of a VM.
type ShareInfo struct {
	Port   int    `json:"port"`
	Status string `json:"status"`
	URL    string `json:"url"`
	VMName string `json:"vm_name"`
}

// ShareShow returns the sharing status for a VM.
func (c *Client) ShareShow(ctx context.Context, vmName string) (ShareInfo, error) {
	out, err := c.ExecWithPerm(ctx, "share show "+vmName+" --json", "share show")
	if err != nil {
		return ShareInfo{}, errors.Wrap(err, "share show")
	}
	var info ShareInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return ShareInfo{}, errors.Wrap(err, "parse share json")
	}
	return info, nil
}

// BrowserLink returns a magic link that grants access to the exe.dev dashboard.
func (c *Client) BrowserLink(ctx context.Context) (string, error) {
	out, err := c.Exec(ctx, "browser --json")
	if err != nil {
		return "", errors.Wrap(err, "browser link")
	}
	var resp struct {
		MagicLink string `json:"magic_link"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return "", errors.Wrap(err, "parse browser json")
	}
	return resp.MagicLink, nil
}
