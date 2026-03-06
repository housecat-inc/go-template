package oidc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckAccess(t *testing.T) {
	a := assert.New(t)

	// both empty = open access
	a.True(CheckAccess("anyone@gmail.com", "", ""))

	// domain only
	a.True(CheckAccess("alice@housecat.com", "housecat.com", ""))
	a.False(CheckAccess("alice@gmail.com", "housecat.com", ""))

	// emails only
	a.True(CheckAccess("bob@gmail.com", "", "bob@gmail.com,carol@corp.co"))
	a.True(CheckAccess("carol@corp.co", "", "bob@gmail.com,carol@corp.co"))
	a.False(CheckAccess("eve@evil.com", "", "bob@gmail.com,carol@corp.co"))

	// domain + emails (union)
	a.True(CheckAccess("alice@housecat.com", "housecat.com", "guest@gmail.com"))
	a.True(CheckAccess("guest@gmail.com", "housecat.com", "guest@gmail.com"))
	a.False(CheckAccess("rando@gmail.com", "housecat.com", "guest@gmail.com"))

	// case insensitive
	a.True(CheckAccess("Alice@HouseCat.com", "housecat.com", ""))
	a.True(CheckAccess("BOB@gmail.com", "", "bob@gmail.com"))

	// empty email = denied
	a.False(CheckAccess("", "housecat.com", ""))

	// whitespace handling
	a.True(CheckAccess(" alice@housecat.com ", "housecat.com", ""))
	a.True(CheckAccess("bob@gmail.com", "", " bob@gmail.com , carol@corp.co "))
}
