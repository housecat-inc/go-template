package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/lmittmann/tint"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	slog.SetDefault(slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		TimeFormat: time.Kitchen,
	})))

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

const defaultServer = "https://hc-auth-dev.exe.xyz/"

const usage = `housecat - MCP stdio proxy and CLI client

Usage:
  housecat [flags] <command>

Commands:
  stdio                     Run as stdio MCP proxy server
  tools                     List available tools
  call <tool> [args.json]   Call a tool and print the result
  login                     Authenticate and cache a token

Flags:
  -server <url>   Remote MCP server URL (default: ` + defaultServer + `)
  -token <token>  Bearer token (or MCP_TOKEN env; omit for OAuth login)

Examples:
  housecat stdio
  housecat tools
  housecat call connections
  housecat call greet '{"name":"world"}'
  housecat login
`

func run() error {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	serverURL := flag.String("server", "", "remote MCP server URL")
	token := flag.String("token", "", "bearer token for authentication")
	flag.Parse()

	if *serverURL == "" {
		*serverURL = os.Getenv("MCP_SERVER_URL")
	}
	if *serverURL == "" {
		*serverURL = defaultServer
	}
	if *token == "" {
		*token = os.Getenv("MCP_TOKEN")
	}
	// Default to /mcp path if no path is specified
	if u, err := url.Parse(*serverURL); err == nil && (u.Path == "" || u.Path == "/") {
		u.Path = "/mcp"
		*serverURL = u.String()
	}
	// Strip trailing slash for consistency
	*serverURL = strings.TrimRight(*serverURL, "/")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// If no token provided, try cache then OAuth login
	if *token == "" {
		if cached, ok := loadCachedToken(ctx, *serverURL); ok {
			slog.Info("using cached token")
			*token = cached
		} else {
			slog.Info("no token provided, starting OAuth login flow")
			t, err := login(ctx, *serverURL)
			if err != nil {
				return fmt.Errorf("login: %w", err)
			}
			*token = t
		}
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	switch args[0] {
	case "login":
		// Just authenticate and exit — token is already cached above
		return nil

	case "stdio":
		remoteSession, err := connectRemote(ctx, *serverURL, *token)
		if err != nil {
			return fmt.Errorf("connect to remote: %w", err)
		}
		defer remoteSession.Close()
		return runStdioProxy(ctx, remoteSession)

	case "tools":
		remoteSession, err := connectRemote(ctx, *serverURL, *token)
		if err != nil {
			return fmt.Errorf("connect to remote: %w", err)
		}
		defer remoteSession.Close()
		return listTools(ctx, remoteSession)

	case "call":
		if len(args) < 2 {
			return fmt.Errorf("usage: housecat call <tool> [args.json]")
		}
		remoteSession, err := connectRemote(ctx, *serverURL, *token)
		if err != nil {
			return fmt.Errorf("connect to remote: %w", err)
		}
		defer remoteSession.Close()
		return callTool(ctx, remoteSession, args[1], args[2:])

	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}
	return nil
}

func connectRemote(ctx context.Context, serverURL, token string) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "housecat",
		Version: "0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: serverURL,
		HTTPClient: &http.Client{
			Transport: &tokenTransport{token: token},
		},
	}, nil)
	if err != nil {
		return nil, err
	}

	slog.Info("connected to remote MCP server", "url", serverURL)
	return session, nil
}

func callTool(ctx context.Context, session *mcp.ClientSession, toolName string, args []string) error {
	var arguments map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal([]byte(args[0]), &arguments); err != nil {
			return fmt.Errorf("invalid arguments JSON: %w", err)
		}
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: arguments,
	})
	if err != nil {
		return fmt.Errorf("call tool %q: %w", toolName, err)
	}

	if result.IsError {
		for _, c := range result.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				fmt.Fprintln(os.Stderr, tc.Text)
			}
		}
		return fmt.Errorf("tool %q returned an error", toolName)
	}

	// If there's structured content, print that directly
	if result.StructuredContent != nil {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result.StructuredContent)
	}

	// Otherwise extract text content — try to pretty-print if it's JSON
	for _, c := range result.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			continue
		}
		var obj any
		if json.Unmarshal([]byte(tc.Text), &obj) == nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(obj); err != nil {
				return fmt.Errorf("encode JSON output: %w", err)
			}
		} else {
			fmt.Println(tc.Text)
		}
	}
	return nil
}

func listTools(ctx context.Context, session *mcp.ClientSession) error {
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	for _, tool := range result.Tools {
		fmt.Printf("%-30s %s\n", tool.Name, tool.Description)
	}
	return nil
}

func runStdioProxy(ctx context.Context, remoteSession *mcp.ClientSession) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "housecat",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Logger: slog.Default(),
	})

	// List tools from remote and register proxy handlers
	toolsResult, err := remoteSession.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list remote tools: %w", err)
	}

	registerProxyTools(server, remoteSession, toolsResult.Tools)

	// Handle paginated tool listings
	for toolsResult.NextCursor != "" {
		toolsResult, err = remoteSession.ListTools(ctx, &mcp.ListToolsParams{
			Cursor: toolsResult.NextCursor,
		})
		if err != nil {
			return fmt.Errorf("list remote tools (page): %w", err)
		}
		registerProxyTools(server, remoteSession, toolsResult.Tools)
	}

	slog.Info("starting stdio transport")
	return server.Run(ctx, &mcp.StdioTransport{})
}

func registerProxyTools(server *mcp.Server, remoteSession *mcp.ClientSession, tools []*mcp.Tool) {
	for _, t := range tools {
		slog.Info("proxying tool", "name", t.Name)
		server.AddTool(t, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return remoteSession.CallTool(ctx, &mcp.CallToolParams{
				Name:      t.Name,
				Arguments: json.RawMessage(req.Params.Arguments),
			})
		})
	}
}

type tokenTransport struct {
	token string
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}
