// +build e2e

/*
Copyright 2018 Knative Authors LLC
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package test

import (
	"strings"
	"testing"

	"github.com/knative/build-pipeline/pkg/apis/pipeline/v1alpha1"
	tb "github.com/knative/build-pipeline/test/builder"
	knativetest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	gitSourceResourceName  = "go-helloworld-git"
	gitTestTaskName        = "git-check-task"
	gitTestPipelineName    = "git-check-pipeline"
	gitTestPipelineRunName = "git-check-pipeline-run"
)

// TestGitPipelineRun is an integration test that will verify the source code is either fetched or pulled
// successfully under different revision inputs (branch, commitid, tag, ref)
func TestGitPipelineRun(t *testing.T) {
	logger := logging.GetContextLogger(t.Name())
	t.Parallel()

	revisions := []string{"c15aced0e5aaee6456fbe6f7a7e95e0b5b3b2b2f", "c15aced", "release-0.1", "v0.1.0", "refs/pull/347/head"}

	for _, revision := range revisions {

		c, namespace := setup(t, logger)
		knativetest.CleanupOnInterrupt(func() { tearDown(t, logger, c, namespace) }, logger)
		defer tearDown(t, logger, c, namespace)

		logger.Infof("Creating Git PipelineResource %s", gitSourceResourceName)
		if _, err := c.PipelineResourceClient.Create(getGitPipelineResource(namespace, revision)); err != nil {
			t.Fatalf("Failed to create Pipeline Resource `%s`: %s", gitSourceResourceName, err)
		}

		logger.Infof("Creating Task %s", gitTestTaskName)
		if _, err := c.TaskClient.Create(getGitCheckTask(namespace, t, logger)); err != nil {
			t.Fatalf("Failed to create Task `%s`: %s", gitTestTaskName, err)
		}

		logger.Infof("Creating Pipeline %s", gitTestPipelineName)
		if _, err := c.PipelineClient.Create(getGitCheckPipeline(namespace)); err != nil {
			t.Fatalf("Failed to create Pipeline `%s`: %s", gitTestPipelineName, err)
		}

		logger.Infof("Creating PipelineRun %s", gitTestPipelineRunName)
		if _, err := c.PipelineRunClient.Create(getGitCheckPipelineRun(namespace)); err != nil {
			t.Fatalf("Failed to create Pipeline `%s`: %s", gitTestPipelineRunName, err)
		}

		// Verify status of PipelineRun (wait for it)
		if err := WaitForPipelineRunState(c, gitTestPipelineRunName, timeout, PipelineRunSucceed(gitTestPipelineRunName), "PipelineRunCompleted"); err != nil {
			t.Errorf("Error waiting for PipelineRun %s to finish: %s", gitTestPipelineRunName, err)
			taskruns, err := c.TaskRunClient.List(metav1.ListOptions{})
			if err != nil {
				t.Errorf("Error getting TaskRun list for PipelineRun %s %s", gitTestPipelineRunName, err)
			}
			for _, tr := range taskruns.Items {
				if tr.Status.PodName != "" {
					CollectBuildLogs(c, tr.Status.PodName, namespace, logger)
				}
			}
			t.Fatalf("PipelineRun execution failed")
		}
	}
}

// TestGitPipelineRunFail is a test to ensure that the code extraction from github fails as expected when
// an invalid revision is passed on the pipelineresource.
func TestGitPipelineRunFail(t *testing.T) {
	logger := logging.GetContextLogger(t.Name())
	t.Parallel()

	c, namespace := setup(t, logger)
	knativetest.CleanupOnInterrupt(func() { tearDown(t, logger, c, namespace) }, logger)
	defer tearDown(t, logger, c, namespace)

	logger.Infof("Creating Git PipelineResource %s", gitSourceResourceName)
	if _, err := c.PipelineResourceClient.Create(getGitPipelineResource(namespace, "Idontexistrabbitmonkeydonkey")); err != nil {
		t.Fatalf("Failed to create Pipeline Resource `%s`: %s", gitSourceResourceName, err)
	}

	logger.Infof("Creating Task %s", gitTestTaskName)
	if _, err := c.TaskClient.Create(getGitCheckTask(namespace, t, logger)); err != nil {
		t.Fatalf("Failed to create Task `%s`: %s", gitTestTaskName, err)
	}

	logger.Infof("Creating Pipeline %s", gitTestPipelineName)
	if _, err := c.PipelineClient.Create(getGitCheckPipeline(namespace)); err != nil {
		t.Fatalf("Failed to create Pipeline `%s`: %s", gitTestPipelineName, err)
	}

	logger.Infof("Creating PipelineRun %s", gitTestPipelineRunName)
	if _, err := c.PipelineRunClient.Create(getGitCheckPipelineRun(namespace)); err != nil {
		t.Fatalf("Failed to create Pipeline `%s`: %s", gitTestPipelineRunName, err)
	}

	if err := WaitForPipelineRunState(c, gitTestPipelineRunName, timeout, PipelineRunSucceed(gitTestPipelineRunName), "PipelineRunCompleted"); err != nil {
		taskruns, err := c.TaskRunClient.List(metav1.ListOptions{})
		if err != nil {
			t.Errorf("Error getting TaskRun list for PipelineRun %s %s", gitTestPipelineRunName, err)
		}
		for _, tr := range taskruns.Items {
			if tr.Status.PodName != "" {
				p, err := c.KubeClient.Kube.CoreV1().Pods(namespace).Get(tr.Status.PodName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Error getting pod `%s` in namespace `%s`", tr.Status.PodName, namespace)
				}

				for _, stat := range p.Status.InitContainerStatuses {
					if strings.HasPrefix(stat.Name, "build-step-git-source-"+gitSourceResourceName) {
						if stat.State.Terminated != nil {
							req := c.KubeClient.Kube.CoreV1().Pods(namespace).GetLogs(p.Name, &corev1.PodLogOptions{Container: stat.Name})
							logContent, err := req.Do().Raw()
							if err != nil {
								t.Fatalf("Error getting pod logs for pod `%s` and container `%s` in namespace `%s`", tr.Status.PodName, stat.Name, namespace)
							}
							// Check for failure messages from fetch and pull in the log file
							if strings.Contains(string(logContent), "Couldn't find remote ref Idontexistrabbitmonkeydonkey") &&
								strings.Contains(string(logContent), "pathspec 'Idontexistrabbitmonkeydonkey' did not match any file(s) known to git") {
								logger.Infof("Found exepected errors when retrieving non-existent git revision")
							} else {
								logger.Infof("Init container `%s` log File: %s", stat.Name, logContent)
								t.Fatalf("The git code extraction did not fail as expected.  Expected errors not found in log file.")
							}
						}
					}
				}
			}
		}

	} else {
		t.Fatalf("PipelineRun succeeded when should have failed")
	}
}

func getGitPipelineResource(namespace, revision string) *v1alpha1.PipelineResource {
	return tb.PipelineResource(gitSourceResourceName, namespace, tb.PipelineResourceSpec(
		v1alpha1.PipelineResourceTypeGit,
		tb.PipelineResourceSpecParam("Url", "https://github.com/knative/build-pipeline"),
		tb.PipelineResourceSpecParam("Revision", revision),
	))
}

func getGitCheckTask(namespace string, t *testing.T, logger *logging.BaseLogger) *v1alpha1.Task {
	return tb.Task(gitTestTaskName, namespace, tb.TaskSpec(
		tb.TaskInputs(tb.InputsResource("gitsource", v1alpha1.PipelineResourceTypeGit)),
		tb.Step("git", "alpine/git", tb.Args("--git-dir=/workspace/gitsource/.git", "show")),
	))
}

func getGitCheckPipeline(namespace string) *v1alpha1.Pipeline {
	return tb.Pipeline(gitTestPipelineName, namespace, tb.PipelineSpec(
		tb.PipelineDeclaredResource("git-repo", "git"),
		tb.PipelineTask("git-check", gitTestTaskName,
			tb.PipelineTaskInputResource("gitsource", "git-repo"),
		),
	))
}

func getGitCheckPipelineRun(namespace string) *v1alpha1.PipelineRun {
	return tb.PipelineRun(gitTestPipelineRunName, namespace, tb.PipelineRunSpec(
		gitTestPipelineName,
		tb.PipelineRunResourceBinding("git-repo", tb.PipelineResourceBindingRef(gitSourceResourceName)),
	))
}
