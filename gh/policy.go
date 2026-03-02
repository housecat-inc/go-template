package gh

import (
	"context"
	"path"
	"strings"

	"github.com/cockroachdb/errors"
)

type Op int

const (
	OpFetch Op = iota
	OpPush
	OpAPIRead
	OpAPIWrite
)

type PolicyStore interface {
	Lookup(ctx context.Context, proxyToken string) (*Policy, error)
}

type Policy struct {
	AllowedOps     []Op
	AllowedRepos   []string
	BranchPrefixes []string
}

func (p *Policy) CheckFetch(repo string) error {
	if !p.hasOp(OpFetch) {
		return errors.New("fetch not allowed")
	}
	return p.checkRepo(repo)
}

func (p *Policy) CheckPush(repo string, refs []RefUpdate) error {
	if !p.hasOp(OpPush) {
		return errors.New("push not allowed")
	}
	if err := p.checkRepo(repo); err != nil {
		return err
	}
	for _, ref := range refs {
		if err := p.checkRef(ref.RefName); err != nil {
			return errors.Wrapf(err, "ref %s", ref.RefName)
		}
	}
	return nil
}

func (p *Policy) CheckAPI(method, urlPath string) error {
	if isWriteMethod(method) {
		if !p.hasOp(OpAPIWrite) {
			return errors.Newf("api write (%s) not allowed", method)
		}
	} else {
		if !p.hasOp(OpAPIRead) {
			return errors.New("api read not allowed")
		}
	}

	repo := repoFromAPIPath(urlPath)
	if repo != "" {
		return p.checkRepo(repo)
	}
	return nil
}

func (p *Policy) checkRepo(repo string) error {
	for _, allowed := range p.AllowedRepos {
		if strings.EqualFold(allowed, repo) {
			return nil
		}
	}
	return errors.Newf("repo %q is not in the proxy's allowed repos list; add it to GH_ALLOWED_REPOS", repo)
}

func (p *Policy) checkRef(refName string) error {
	if len(p.BranchPrefixes) == 0 {
		return errors.New("no branch prefix configured; set a branch prefix for this VM")
	}

	branch := strings.TrimPrefix(refName, "refs/heads/")
	if branch == refName {
		return errors.Newf("only branch refs allowed, got %q", refName)
	}

	for _, prefix := range p.BranchPrefixes {
		matched, _ := path.Match(prefix, branch)
		if matched {
			return nil
		}
		if strings.HasSuffix(prefix, "/*") {
			dir := strings.TrimSuffix(prefix, "/*")
			if strings.HasPrefix(branch, dir+"/") {
				return nil
			}
		}
	}
	return errors.Newf("branch %q not allowed; push to branches matching: %v", branch, p.BranchPrefixes)
}

func (p *Policy) hasOp(op Op) bool {
	for _, o := range p.AllowedOps {
		if o == op {
			return true
		}
	}
	return false
}

func isWriteMethod(method string) bool {
	switch method {
	case "DELETE", "PATCH", "POST", "PUT":
		return true
	}
	return false
}

func repoFromAPIPath(urlPath string) string {
	parts := strings.Split(strings.TrimPrefix(urlPath, "/"), "/")
	if len(parts) >= 3 && parts[0] == "repos" {
		return parts[1] + "/" + parts[2]
	}
	return ""
}

func repoFromGitPath(urlPath string) string {
	p := strings.TrimPrefix(urlPath, "/")
	p = strings.TrimSuffix(p, "/info/refs")
	p = strings.TrimSuffix(p, "/git-upload-pack")
	p = strings.TrimSuffix(p, "/git-receive-pack")
	p = strings.TrimSuffix(p, ".git")
	parts := strings.Split(p, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return ""
}
