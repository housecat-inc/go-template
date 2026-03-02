package gh

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
)

type RefUpdate struct {
	NewHash string
	OldHash string
	RefName string
}

func ParseReceivePackRefs(data []byte) ([]RefUpdate, error) {
	var refs []RefUpdate
	pos := 0

	for pos+4 <= len(data) {
		lenHex := string(data[pos : pos+4])
		if lenHex == "0000" {
			break
		}

		pktLen, err := parseHexLen(lenHex)
		if err != nil {
			return nil, errors.Wrap(err, "parse pkt-line length")
		}

		if pktLen < 4 || pos+pktLen > len(data) {
			return nil, errors.Newf("invalid pkt-line length %d at pos %d", pktLen, pos)
		}

		line := string(data[pos+4 : pos+pktLen])
		pos += pktLen

		if idx := strings.IndexByte(line, 0); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimRight(line, "\n")

		parts := strings.SplitN(line, " ", 3)
		if len(parts) != 3 {
			continue
		}

		refs = append(refs, RefUpdate{
			NewHash: parts[1],
			OldHash: parts[0],
			RefName: parts[2],
		})
	}

	return refs, nil
}

func parseHexLen(s string) (int, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return 0, err
	}
	if len(b) != 2 {
		return 0, errors.New("expected 2 bytes")
	}
	return int(b[0])<<8 | int(b[1]), nil
}

func pktLine(s string) []byte {
	n := len(s) + 4
	return []byte(fmt.Sprintf("%04x%s", n, s))
}

func ReceivePackReject(refs []RefUpdate, reason string) []byte {
	var buf []byte
	buf = append(buf, pktLine("unpack ok\n")...)
	for _, ref := range refs {
		buf = append(buf, pktLine(fmt.Sprintf("ng %s %s\n", ref.RefName, reason))...)
	}
	buf = append(buf, "0000"...)
	return buf
}
