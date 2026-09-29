package runner

import (
	"archive/tar"
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/nektos/act/pkg/common/git"
	"github.com/nektos/act/pkg/model"
)

// repositorySource identifies the repository revision containing a workflow
// or action. A source is either a directory or an exact revision in an
// ActionCache.
type repositorySource struct {
	directory   string
	actionCache ActionCache
	cacheDir    string
	sha         string
	repository  string
	ref         string
}

func (source repositorySource) revision() string {
	if source.sha != "" {
		return source.sha
	}
	return source.ref
}

func (rc *RunContext) resolveRepositorySource(ctx context.Context) (repositorySource, error) {
	source := rc.repositorySource
	if source.directory == "" && source.actionCache == nil {
		source.directory = rc.Config.Workdir
	}

	if source.repository == "" {
		github := rc.getGithubContext(ctx)
		source.repository = github.Repository
		source.ref = github.Ref
		source.sha = github.Sha
	}
	if source.sha == "" && source.directory != "" {
		if _, sha, err := git.FindGitRevision(ctx, source.directory); err == nil {
			source.sha = sha
		}
	}

	if source.repository == "" {
		return repositorySource{}, fmt.Errorf("unable to resolve the repository for self repository reference")
	}
	if source.revision() == "" {
		return repositorySource{}, fmt.Errorf("unable to resolve the revision for self repository reference in '%s'", source.repository)
	}

	rc.repositorySource = source
	return source, nil
}

func (source repositorySource) remoteAction(actionPath string) (*remoteAction, error) {
	org, repo, found := strings.Cut(source.repository, "/")
	if !found || org == "" || repo == "" || strings.Contains(repo, "/") {
		return nil, fmt.Errorf("invalid repository '%s' for self repository reference", source.repository)
	}

	return &remoteAction{
		Org:  org,
		Repo: repo,
		Path: actionPath,
		Ref:  source.revision(),
		URL:  "https://github.com",
	}, nil
}

func (source repositorySource) workflowPlanner(ctx context.Context, workflow string) (model.WorkflowPlanner, error) {
	if source.actionCache == nil {
		return model.NewWorkflowPlanner(path.Join(source.directory, workflow), true, false)
	}

	workflow = path.Clean(workflow)
	archive, err := source.actionCache.GetTarArchive(ctx, source.cacheDir, source.sha, workflow)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	treader := tar.NewReader(archive)
	if _, err := treader.Next(); err != nil {
		return nil, err
	}

	return model.NewSingleWorkflowPlanner(path.Base(workflow), treader)
}
