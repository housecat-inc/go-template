package exedev

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"

	"github.com/cockroachdb/errors"
	"golang.org/x/crypto/ssh"
)

// GenerateKey creates a new ed25519 key pair and writes it to disk.
// The private key is written with 0600 permissions.
// Registration is a separate manual step: cat <pubPath> | ssh exe.dev ssh-key add
func GenerateKey(privPath, pubPath string) error {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return errors.Wrap(err, "generate ed25519 key")
	}

	privPEM, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return errors.Wrap(err, "marshal private key")
	}
	if err := os.WriteFile(privPath, pem.EncodeToMemory(privPEM), 0600); err != nil {
		return errors.Wrap(err, "write private key")
	}

	sshPub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return errors.Wrap(err, "create public key")
	}
	if err := os.WriteFile(pubPath, ssh.MarshalAuthorizedKey(sshPub), 0644); err != nil {
		return errors.Wrap(err, "write public key")
	}
	return nil
}
