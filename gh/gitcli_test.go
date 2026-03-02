package gh

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeGitUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/info/refs") {
			svc := r.URL.Query().Get("service")
			w.Header().Set("Content-Type", fmt.Sprintf("application/x-%s-advertisement", svc))
			var lines []byte
			lines = append(lines, pktLine(fmt.Sprintf("# service=%s\n", svc))...)
			lines = append(lines, []byte("0000")...)
			zeroHash := "0000000000000000000000000000000000000000"
			if svc == "git-receive-pack" {
				lines = append(lines, pktLine(zeroHash+" capabilities^{}\x00report-status delete-refs side-band-64k ofs-delta\n")...)
			} else {
				lines = append(lines, pktLine(zeroHash+" capabilities^{}\x00multi_ack side-band-64k ofs-delta\n")...)
			}
			lines = append(lines, []byte("0000")...)
			_, _ = w.Write(lines)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

func gitPush(t *testing.T, repoDir, remoteURL, branch, user, pass string) (string, error) {
	t.Helper()

	credHelper := fmt.Sprintf("!f() { echo username=%s; echo password=%s; }; f", user, pass)

	cmd := exec.Command("git", "push", remoteURL, "main:"+branch)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		fmt.Sprintf("GIT_CONFIG_COUNT=1"),
		fmt.Sprintf("GIT_CONFIG_KEY_0=credential.helper"),
		fmt.Sprintf("GIT_CONFIG_VALUE_0=%s", credHelper),
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestGitCLI_PushDisallowedBranch(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	upstream := fakeGitUpstream(t)
	defer upstream.Close()

	store := &staticPolicyStore{
		policies: map[string]*Policy{
			"cid:csecret": {
				AllowedOps:     []Op{OpFetch, OpPush},
				AllowedRepos:   []string{"org/repo"},
				BranchPrefixes: []string{"test-vm/*"},
			},
		},
	}

	_, ps := testProxy(t, upstream, store)
	repoDir := initGitRepo(t)

	out, err := gitPush(t, repoDir, ps.URL+"/github.com/org/repo.git", "not-allowed-branch", "cid", "csecret")
	r.Error(err, "push should fail")
	a.Contains(out, "not allowed", "git output should contain policy error, got: %s", out)
	a.NotContains(out, "bad band", "should not get protocol error, got: %s", out)
}

func TestGitCLI_PushNoBranchPrefix(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	upstream := fakeGitUpstream(t)
	defer upstream.Close()

	store := &staticPolicyStore{
		policies: map[string]*Policy{
			"cid:csecret": {
				AllowedOps:     []Op{OpFetch, OpPush},
				AllowedRepos:   []string{"org/repo"},
				BranchPrefixes: nil,
			},
		},
	}

	_, ps := testProxy(t, upstream, store)
	repoDir := initGitRepo(t)

	out, err := gitPush(t, repoDir, ps.URL+"/github.com/org/repo.git", "any-branch", "cid", "csecret")
	r.Error(err, "push should fail when no branch prefix is configured")
	a.Contains(out, "branch prefix", "git output should tell user to set branch prefix, got: %s", out)
	a.NotContains(out, "bad band", "should not get protocol error, got: %s", out)
}
