package gh

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPolicyCheckPush(t *testing.T) {
	a := assert.New(t)

	p := &Policy{
		AllowedOps:     []Op{OpFetch, OpPush},
		AllowedRepos:   []string{"housecat-inc/auth"},
		BranchPrefixes: []string{"shelley/*"},
	}

	a.NoError(p.CheckPush("housecat-inc/auth", []RefUpdate{
		{RefName: "refs/heads/shelley/my-feature"},
	}))

	a.Error(p.CheckPush("housecat-inc/auth", []RefUpdate{
		{RefName: "refs/heads/main"},
	}))

	a.Error(p.CheckPush("other-org/other-repo", []RefUpdate{
		{RefName: "refs/heads/shelley/my-feature"},
	}))

	a.Error(p.CheckPush("housecat-inc/auth", []RefUpdate{
		{RefName: "refs/tags/v1.0.0"},
	}))
}

func TestPolicyCheckFetch(t *testing.T) {
	a := assert.New(t)

	p := &Policy{
		AllowedOps:   []Op{OpFetch},
		AllowedRepos: []string{"housecat-inc/auth"},
	}

	a.NoError(p.CheckFetch("housecat-inc/auth"))
	a.Error(p.CheckFetch("housecat-inc/other"))
}

func TestPolicyCheckFetchDeniedWhenNotAllowed(t *testing.T) {
	a := assert.New(t)

	p := &Policy{
		AllowedOps:   []Op{OpPush},
		AllowedRepos: []string{"housecat-inc/auth"},
	}

	a.Error(p.CheckFetch("housecat-inc/auth"))
}

func TestPolicyCheckAPI(t *testing.T) {
	a := assert.New(t)

	p := &Policy{
		AllowedOps:   []Op{OpAPIRead, OpAPIWrite},
		AllowedRepos: []string{"housecat-inc/auth"},
	}

	a.NoError(p.CheckAPI("GET", "/repos/housecat-inc/auth/pulls"))
	a.NoError(p.CheckAPI("POST", "/repos/housecat-inc/auth/pulls"))
	a.Error(p.CheckAPI("GET", "/repos/housecat-inc/other/pulls"))
}

func TestPolicyCheckAPIReadOnly(t *testing.T) {
	a := assert.New(t)

	p := &Policy{
		AllowedOps:   []Op{OpAPIRead},
		AllowedRepos: []string{"housecat-inc/auth"},
	}

	a.NoError(p.CheckAPI("GET", "/repos/housecat-inc/auth/pulls"))
	a.Error(p.CheckAPI("POST", "/repos/housecat-inc/auth/pulls"))
}

func TestRepoFromGitPath(t *testing.T) {
	a := assert.New(t)

	a.Equal("housecat-inc/auth", repoFromGitPath("/housecat-inc/auth.git/git-receive-pack"))
	a.Equal("housecat-inc/auth", repoFromGitPath("/housecat-inc/auth.git/info/refs"))
	a.Equal("housecat-inc/auth", repoFromGitPath("/housecat-inc/auth.git/git-upload-pack"))
}

func TestRepoFromAPIPath(t *testing.T) {
	a := assert.New(t)

	a.Equal("housecat-inc/auth", repoFromAPIPath("/repos/housecat-inc/auth/pulls"))
	a.Equal("housecat-inc/auth", repoFromAPIPath("/repos/housecat-inc/auth"))
	a.Equal("", repoFromAPIPath("/user"))
}

func TestPolicyMultipleBranchPrefixes(t *testing.T) {
	a := assert.New(t)

	p := &Policy{
		AllowedOps:     []Op{OpPush},
		AllowedRepos:   []string{"housecat-inc/auth"},
		BranchPrefixes: []string{"shelley/*", "deploy/*"},
	}

	a.NoError(p.CheckPush("housecat-inc/auth", []RefUpdate{
		{RefName: "refs/heads/shelley/feat"},
	}))
	a.NoError(p.CheckPush("housecat-inc/auth", []RefUpdate{
		{RefName: "refs/heads/deploy/v1"},
	}))
	a.Error(p.CheckPush("housecat-inc/auth", []RefUpdate{
		{RefName: "refs/heads/feature/foo"},
	}))
}
