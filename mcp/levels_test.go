package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func mockLookup(connected map[string]bool) TokenLookup {
	return func(ctx context.Context, subject, service, level string) (string, error) {
		if connected[level] {
			return "tok-" + level, nil
		}
		return "", ErrTokenNotFound
	}
}

func resolve(lookup TokenLookup, levels []string) (string, string, error) {
	for _, level := range levels {
		token, err := lookup(context.Background(), "user", "svc", level)
		if err != nil {
			continue
		}
		if token != "" {
			return token, level, nil
		}
	}
	return "", "", ErrTokenNotFound
}

func TestLevelFallback(t *testing.T) {
	a := assert.New(t)
	a.Equal([]string{"read", "draft", "write"}, levelFallback("read"))
	a.Equal([]string{"write", "draft"}, levelFallback("draft"))
	a.Equal([]string{"write"}, levelFallback("write"))
	a.Equal([]string{"archive"}, levelFallback("archive"))
}

func TestReadLevelResolution(t *testing.T) {
	levels := levelFallback("read")

	t.Run("finds read", func(t *testing.T) {
		_, level, err := resolve(mockLookup(map[string]bool{"read": true}), levels)
		assert.NoError(t, err)
		assert.Equal(t, "read", level)
	})

	t.Run("falls back to draft", func(t *testing.T) {
		_, level, err := resolve(mockLookup(map[string]bool{"draft": true}), levels)
		assert.NoError(t, err)
		assert.Equal(t, "draft", level)
	})

	t.Run("falls back to write", func(t *testing.T) {
		_, level, err := resolve(mockLookup(map[string]bool{"write": true}), levels)
		assert.NoError(t, err)
		assert.Equal(t, "write", level)
	})

	t.Run("does not find archive", func(t *testing.T) {
		_, _, err := resolve(mockLookup(map[string]bool{"archive": true}), levels)
		assert.Error(t, err)
	})
}

func TestDraftLevelResolution(t *testing.T) {
	levels := levelFallback("draft")

	t.Run("prefers write over draft", func(t *testing.T) {
		_, level, err := resolve(mockLookup(map[string]bool{"draft": true, "write": true}), levels)
		assert.NoError(t, err)
		assert.Equal(t, "write", level)
	})

	t.Run("finds draft alone", func(t *testing.T) {
		_, level, err := resolve(mockLookup(map[string]bool{"draft": true}), levels)
		assert.NoError(t, err)
		assert.Equal(t, "draft", level)
	})

	t.Run("does not find archive", func(t *testing.T) {
		_, _, err := resolve(mockLookup(map[string]bool{"archive": true}), levels)
		assert.Error(t, err)
	})

	t.Run("does not find read", func(t *testing.T) {
		_, _, err := resolve(mockLookup(map[string]bool{"read": true}), levels)
		assert.Error(t, err)
	})

	t.Run("none connected errors", func(t *testing.T) {
		_, _, err := resolve(mockLookup(map[string]bool{}), levels)
		assert.Error(t, err)
	})
}

func TestDeleteLevelResolution(t *testing.T) {
	levels := []string{"archive", "draft"}

	t.Run("prefers archive over draft", func(t *testing.T) {
		_, level, err := resolve(mockLookup(map[string]bool{"archive": true, "draft": true}), levels)
		assert.NoError(t, err)
		assert.Equal(t, "archive", level)
	})

	t.Run("draft alone works", func(t *testing.T) {
		_, level, err := resolve(mockLookup(map[string]bool{"draft": true}), levels)
		assert.NoError(t, err)
		assert.Equal(t, "draft", level)
	})

	t.Run("write only does not allow delete", func(t *testing.T) {
		_, _, err := resolve(mockLookup(map[string]bool{"write": true}), levels)
		assert.Error(t, err)
	})

	t.Run("write+draft uses draft for delete", func(t *testing.T) {
		_, level, err := resolve(mockLookup(map[string]bool{"write": true, "draft": true}), levels)
		assert.NoError(t, err)
		assert.Equal(t, "draft", level)
	})

	t.Run("read only does not allow delete", func(t *testing.T) {
		_, _, err := resolve(mockLookup(map[string]bool{"read": true}), levels)
		assert.Error(t, err)
	})

	t.Run("none connected errors", func(t *testing.T) {
		_, _, err := resolve(mockLookup(map[string]bool{}), levels)
		assert.Error(t, err)
	})
}
