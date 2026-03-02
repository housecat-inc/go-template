package srv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateName(t *testing.T) {
	a := assert.New(t)

	for range 100 {
		name := GenerateName()
		parts := strings.SplitN(name, "-", 2)
		a.Len(parts, 2, "name should have adj-noun format: %s", name)
		a.NotEmpty(parts[0])
		a.NotEmpty(parts[1])
	}
}

func TestGenerateNameVariety(t *testing.T) {
	a := assert.New(t)

	seen := map[string]bool{}
	for range 200 {
		seen[GenerateName()] = true
	}
	a.Greater(len(seen), 50, "should generate diverse names")
}
