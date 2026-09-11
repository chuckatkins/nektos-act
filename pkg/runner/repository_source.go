package runner

import (
	"archive/tar"
	"context"
	"path"

	"github.com/nektos/act/pkg/model"
)

// repositorySource identifies the repository revision containing a reusable
// workflow. A source is either a directory or an exact revision in an
// ActionCache.
type repositorySource struct {
	directory   string
	actionCache ActionCache
	cacheDir    string
	sha         string
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
