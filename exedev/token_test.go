package exedev

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func testSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	return signer
}

func TestSignToken(t *testing.T) {
	signer := testSigner(t)

	token, err := SignToken(signer, []string{"ls", "browser"}, 5*time.Minute)
	require.NoError(t, err)

	// Token format: exe0.PAYLOAD.SIGBLOB
	parts := strings.SplitN(token, ".", 3)
	require.Len(t, parts, 3, "token should have 3 dot-separated parts")
	assert.Equal(t, "exe0", parts[0])

	// Decode and verify payload
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)

	var perms Permissions
	require.NoError(t, json.Unmarshal(payloadBytes, &perms))
	assert.Equal(t, []string{"ls", "browser"}, perms.Cmds)
	assert.Greater(t, perms.Exp, time.Now().Unix(), "expiry should be in the future")

	// Verify sigblob decodes as valid base64url
	sigBlob, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	assert.NotEmpty(t, sigBlob)

	// Verify the SSHSIG blob starts with the magic preamble
	assert.True(t, strings.HasPrefix(string(sigBlob), sshsigMagic),
		"SSHSIG blob should start with %q", sshsigMagic)
}

func TestSignTokenDeterministicPayload(t *testing.T) {
	signer := testSigner(t)

	// Two tokens with the same params should have the same payload structure
	token1, err := SignToken(signer, []string{"ls"}, 5*time.Minute)
	require.NoError(t, err)
	token2, err := SignToken(signer, []string{"ls"}, 5*time.Minute)
	require.NoError(t, err)

	// Payloads may differ slightly (exp timestamp), but both should be valid
	parts1 := strings.SplitN(token1, ".", 3)
	parts2 := strings.SplitN(token2, ".", 3)

	p1, _ := base64.RawURLEncoding.DecodeString(parts1[1])
	p2, _ := base64.RawURLEncoding.DecodeString(parts2[1])

	var perms1, perms2 Permissions
	require.NoError(t, json.Unmarshal(p1, &perms1))
	require.NoError(t, json.Unmarshal(p2, &perms2))

	assert.Equal(t, perms1.Cmds, perms2.Cmds)
}

func TestExtractCmdName(t *testing.T) {
	assert.Equal(t, "ls", extractCmdName("ls --json"))
	assert.Equal(t, "browser", extractCmdName("browser my-vm"))
	assert.Equal(t, "whoami", extractCmdName("whoami"))
}
