package gcpdns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/cockroachdb/errors"
	"golang.org/x/oauth2/google"
)

const (
	cnameZone  = "vm.housecat.io"
	targetZone = "exe.xyz"
	ttl        = 300
)

type Client struct {
	httpClient *http.Client
	project    string
	zone       string
}

func New(saKeyPath, project, zone string) (*Client, error) {
	keyBytes, err := os.ReadFile(saKeyPath)
	if err != nil {
		return nil, errors.Wrap(err, "read sa key")
	}
	cfg, err := google.JWTConfigFromJSON(keyBytes, "https://www.googleapis.com/auth/ndev.clouddns.readwrite")
	if err != nil {
		return nil, errors.Wrap(err, "parse sa key")
	}
	return &Client{
		httpClient: cfg.Client(context.Background()),
		project:    project,
		zone:       zone,
	}, nil
}

func (c *Client) CreateCNAME(ctx context.Context, vmName string) error {
	return c.postChange(ctx, "additions", vmName)
}

func (c *Client) DeleteCNAME(ctx context.Context, vmName string) error {
	return c.postChange(ctx, "deletions", vmName)
}

type change struct {
	Additions []recordSet `json:"additions,omitempty"`
	Deletions []recordSet `json:"deletions,omitempty"`
}

type recordSet struct {
	Name    string   `json:"name"`
	Rrdatas []string `json:"rrdatas"`
	TTL     int      `json:"ttl"`
	Type    string   `json:"type"`
}

func (c *Client) postChange(ctx context.Context, op string, vmName string) error {
	rs := recordSet{
		Name:    vmName + "." + cnameZone + ".",
		Rrdatas: []string{vmName + "." + targetZone + "."},
		TTL:     ttl,
		Type:    "CNAME",
	}

	var ch change
	switch op {
	case "additions":
		ch.Additions = []recordSet{rs}
	case "deletions":
		ch.Deletions = []recordSet{rs}
	}

	body, err := json.Marshal(ch)
	if err != nil {
		return errors.Wrap(err, "marshal change")
	}

	url := fmt.Sprintf("https://dns.googleapis.com/dns/v1/projects/%s/managedZones/%s/changes", c.project, c.zone)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "create request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "dns api request")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return errors.Newf("dns api %s: %d: %s", op, resp.StatusCode, string(respBody))
	}
	return nil
}
