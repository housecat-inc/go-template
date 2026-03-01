# Agent Instructions

This is a Go web application template for exe.dev.

See README.md for details on the structure and components.

## Code Conventions

Don't add comments, write self-commenting code.

Sort fields lexigrapically when possible (structs, db columns, etc.).

Use `In` and `Out` structs.

## Dependencies

Use `github.com/cockroachdb/errors` and return wrapped errors: `return out, errors.Wrap(err, "foo")`, `return errors.Wrap(foo(), "foo")`
Use `github.com/stretchr/testify/assert`: `a := assert.New()`, `r := require.New()`
