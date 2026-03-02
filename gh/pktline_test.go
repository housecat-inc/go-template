package gh

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReceivePackRefs(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	oldHash := "0000000000000000000000000000000000000000"
	newHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ref := "refs/heads/shelley/my-feature"

	line := oldHash + " " + newHash + " " + ref + "\x00report-status\n"
	pktLen := len(line) + 4
	pkt := []byte{byte(pktLen >> 12), byte(pktLen >> 8), byte(pktLen >> 4), byte(pktLen)}
	// encode as hex properly
	pktHex := make([]byte, 4)
	pktHex[0] = hexChar(pktLen >> 12 & 0xf)
	pktHex[1] = hexChar(pktLen >> 8 & 0xf)
	pktHex[2] = hexChar(pktLen >> 4 & 0xf)
	pktHex[3] = hexChar(pktLen & 0xf)

	_ = pkt
	data := append(pktHex, []byte(line)...)
	data = append(data, []byte("0000")...)

	refs, err := ParseReceivePackRefs(data)
	r.NoError(err)
	a.Len(refs, 1)
	a.Equal(oldHash, refs[0].OldHash)
	a.Equal(newHash, refs[0].NewHash)
	a.Equal(ref, refs[0].RefName)
}

func TestParseReceivePackRefsMultiple(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	lines := []string{
		"0000000000000000000000000000000000000000 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa refs/heads/shelley/feat-1\x00report-status\n",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb cccccccccccccccccccccccccccccccccccccccc refs/heads/shelley/feat-2\n",
	}

	var data []byte
	for _, line := range lines {
		pktLen := len(line) + 4
		data = append(data, []byte(pktLenHex(pktLen))...)
		data = append(data, []byte(line)...)
	}
	data = append(data, []byte("0000")...)

	refs, err := ParseReceivePackRefs(data)
	r.NoError(err)
	a.Len(refs, 2)
	a.Equal("refs/heads/shelley/feat-1", refs[0].RefName)
	a.Equal("refs/heads/shelley/feat-2", refs[1].RefName)
}

func hexChar(b int) byte {
	if b < 10 {
		return byte('0' + b)
	}
	return byte('a' + b - 10)
}

func pktLenHex(n int) string {
	return string([]byte{
		hexChar(n >> 12 & 0xf),
		hexChar(n >> 8 & 0xf),
		hexChar(n >> 4 & 0xf),
		hexChar(n & 0xf),
	})
}
