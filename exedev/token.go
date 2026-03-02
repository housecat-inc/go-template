package exedev

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"golang.org/x/crypto/ssh"
)

const (
	sshsigMagic     = "SSHSIG"
	sshsigVersion   = 1
	sshsigNamespace = "v0@exe.dev"
	sshsigHashAlgo  = "sha512"
)

// Permissions defines the claims embedded in an exe0 token.
type Permissions struct {
	Exp  int64    `json:"exp"`
	Cmds []string `json:"cmds"`
}

// SignToken creates an exe0 token: exe0.$PAYLOAD.$SIGBLOB
//
// PAYLOAD is base64url(permissions JSON, no whitespace).
// SIGBLOB is base64url(SSHSIG binary blob) signed with namespace "v0@exe.dev".
func SignToken(signer ssh.Signer, cmds []string, ttl time.Duration) (string, error) {
	perms := Permissions{
		Exp:  time.Now().Add(ttl).Unix(),
		Cmds: cmds,
	}
	permsJSON, err := json.Marshal(perms)
	if err != nil {
		return "", errors.Wrap(err, "marshal permissions")
	}

	payload := base64.RawURLEncoding.EncodeToString(permsJSON)

	sigBlob, err := sshsigSign(signer, sshsigNamespace, permsJSON)
	if err != nil {
		return "", errors.Wrap(err, "sshsig sign")
	}

	sigB64 := base64.RawURLEncoding.EncodeToString(sigBlob)
	return "exe0." + payload + "." + sigB64, nil
}

// sshsigSign produces the binary SSHSIG blob (the content between
// -----BEGIN SSH SIGNATURE----- / -----END SSH SIGNATURE----- armor lines).
func sshsigSign(signer ssh.Signer, namespace string, data []byte) ([]byte, error) {
	// Hash the message.
	h := sha512.Sum512(data)

	// Build the data-to-sign structure per SSHSIG spec:
	//   "SSHSIG" || string(namespace) || string(reserved) || string(hash_algo) || string(H(message))
	signedData := buildSSHSIGSignedData(namespace, h[:])

	// Sign with the SSH key.
	sig, err := signer.Sign(rand.Reader, signedData)
	if err != nil {
		return nil, errors.Wrap(err, "ssh sign")
	}

	// Build the full SSHSIG blob:
	//   "SSHSIG"
	//   uint32  version = 1
	//   string  publickey
	//   string  namespace
	//   string  reserved = ""
	//   string  hash_algorithm
	//   string  signature (wire format)
	return buildSSHSIGBlob(signer.PublicKey(), namespace, sig), nil
}

func buildSSHSIGSignedData(namespace string, messageHash []byte) []byte {
	var buf []byte
	buf = append(buf, []byte(sshsigMagic)...)
	buf = appendSSHString(buf, []byte(namespace))
	buf = appendSSHString(buf, nil) // reserved
	buf = appendSSHString(buf, []byte(sshsigHashAlgo))
	buf = appendSSHString(buf, messageHash)
	return buf
}

func buildSSHSIGBlob(pubKey ssh.PublicKey, namespace string, sig *ssh.Signature) []byte {
	var buf []byte

	// Magic preamble
	buf = append(buf, []byte(sshsigMagic)...)

	// Version
	v := make([]byte, 4)
	binary.BigEndian.PutUint32(v, sshsigVersion)
	buf = append(buf, v...)

	// Public key (wire format, wrapped as SSH string)
	buf = appendSSHString(buf, pubKey.Marshal())

	// Namespace
	buf = appendSSHString(buf, []byte(namespace))

	// Reserved
	buf = appendSSHString(buf, nil)

	// Hash algorithm
	buf = appendSSHString(buf, []byte(sshsigHashAlgo))

	// Signature (wire format, wrapped as SSH string)
	sigWire := marshalSignature(sig)
	buf = appendSSHString(buf, sigWire)

	return buf
}

func marshalSignature(sig *ssh.Signature) []byte {
	// SSH signature wire format: string(format) || string(blob)
	var buf []byte
	buf = appendSSHString(buf, []byte(sig.Format))
	buf = appendSSHString(buf, sig.Blob)
	return buf
}

func appendSSHString(buf, s []byte) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(len(s)))
	buf = append(buf, b...)
	buf = append(buf, s...)
	return buf
}

// extractCmdName extracts the first word from a command string.
func extractCmdName(command string) string {
	if i := strings.IndexByte(command, ' '); i != -1 {
		return command[:i]
	}
	return command
}
