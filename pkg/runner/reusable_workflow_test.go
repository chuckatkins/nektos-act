package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/nektos/act/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	outerReusableWorkflow = `name: outer
on: workflow_call
jobs:
  middle:
    uses: ./.github/workflows/middle.yml
`
	middleReusableWorkflow = `name: middle
on: workflow_call
jobs:
  leaf:
    uses: ./.github/workflows/leaf.yml
`
	leafReusableWorkflow = `name: leaf
on: workflow_call
jobs: {}
`
)

func TestRemoteReusableWorkflowLocalReferencesUseRemoteRepository(t *testing.T) {
	workdir := t.TempDir()
	actionCacheDir := t.TempDir()
	remoteRepository := filepath.Join(actionCacheDir, safeFilename("owner/repo@ci"))
	workflowDirectory := filepath.Join(remoteRepository, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowDirectory, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDirectory, "outer.yml"), []byte(outerReusableWorkflow), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDirectory, "middle.yml"), []byte(middleReusableWorkflow), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDirectory, "leaf.yml"), []byte(leafReusableWorkflow), 0o600))

	rc := reusableWorkflowRunContext("owner/repo/.github/workflows/outer.yml@ci", &Config{
		Workdir:        workdir,
		ActionCacheDir: actionCacheDir,
		ConcurrentJobs: 1,
	})

	require.NoError(t, newRemoteReusableWorkflowExecutor(rc)(context.Background()))
}

type recordingWorkflowCache struct {
	workflows []string
	fetches   int
	reads     []workflowCacheRead
}

type workflowCacheRead struct {
	cacheDir string
	sha      string
	path     string
}

func (cache *recordingWorkflowCache) Fetch(context.Context, string, string, string, string) (string, error) {
	cache.fetches++
	return "resolved-ci-sha", nil
}

func (cache *recordingWorkflowCache) GetTarArchive(_ context.Context, cacheDir, sha, includePrefix string) (io.ReadCloser, error) {
	cache.reads = append(cache.reads, workflowCacheRead{cacheDir: cacheDir, sha: sha, path: includePrefix})
	if len(cache.workflows) == 0 {
		return nil, fmt.Errorf("unexpected workflow read: %s", includePrefix)
	}

	workflow := cache.workflows[0]
	cache.workflows = cache.workflows[1:]
	var archive bytes.Buffer
	twriter := tar.NewWriter(&archive)
	if err := twriter.WriteHeader(&tar.Header{Name: includePrefix, Mode: 0o644, Size: int64(len(workflow))}); err != nil {
		return nil, err
	}
	if _, err := twriter.Write([]byte(workflow)); err != nil {
		return nil, err
	}
	if err := twriter.Close(); err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(archive.Bytes())), nil
}

func TestCachedRemoteReusableWorkflowLocalReferencesReuseResolvedRevision(t *testing.T) {
	cache := &recordingWorkflowCache{
		workflows: []string{outerReusableWorkflow, middleReusableWorkflow, leafReusableWorkflow},
	}
	rc := reusableWorkflowRunContext("owner/repo/.github/workflows/outer.yml@ci", &Config{
		Workdir:        t.TempDir(),
		ActionCache:    cache,
		ConcurrentJobs: 1,
	})

	require.NoError(t, newRemoteReusableWorkflowExecutor(rc)(context.Background()))
	assert.Equal(t, 1, cache.fetches)
	assert.Equal(t, []workflowCacheRead{
		{cacheDir: "owner/repo@ci", sha: "resolved-ci-sha", path: ".github/workflows/outer.yml"},
		{cacheDir: "owner/repo@ci", sha: "resolved-ci-sha", path: ".github/workflows/middle.yml"},
		{cacheDir: "owner/repo@ci", sha: "resolved-ci-sha", path: ".github/workflows/leaf.yml"},
	}, cache.reads)
}

func reusableWorkflowRunContext(uses string, config *Config) *RunContext {
	workflow := &model.Workflow{
		Name: "caller",
		Jobs: map[string]*model.Job{
			"caller": {Uses: uses},
		},
	}
	return &RunContext{
		Config:      config,
		Run:         &model.Run{Workflow: workflow, JobID: "caller"},
		EventJSON:   "{}",
		StepResults: map[string]*model.StepResult{},
	}
}
