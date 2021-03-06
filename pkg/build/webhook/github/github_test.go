package github

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kapi "github.com/GoogleCloudPlatform/kubernetes/pkg/api"

	"github.com/openshift/origin/pkg/build/api"
	"github.com/openshift/origin/pkg/build/webhook"
)

type okBuildConfigGetter struct{}

func (c *okBuildConfigGetter) Get(namespace, name string) (*api.BuildConfig, error) {
	return &api.BuildConfig{
		Triggers: []api.BuildTriggerPolicy{
			{
				Type: api.GithubWebHookBuildTriggerType,
				GithubWebHook: &api.WebHookTrigger{
					Secret: "secret101",
				},
			},
		},
		Parameters: api.BuildParameters{
			Source: api.BuildSource{
				Type: api.BuildSourceGit,
				Git: &api.GitBuildSource{
					URI: "git://github.com/my/repo.git",
				},
			},
			Strategy: mockBuildStrategy,
		},
	}, nil
}

var mockBuildStrategy api.BuildStrategy = api.BuildStrategy{
	Type: "STI",
	STIStrategy: &api.STIBuildStrategy{
		From: &kapi.ObjectReference{
			Kind: "DockerImage",
			Name: "repository/image",
		},
	},
}

type okBuildConfigInstantiator struct{}

func (*okBuildConfigInstantiator) Instantiate(namespace string, requet *api.BuildRequest) (*api.Build, error) {
	return &api.Build{}, nil
}

func TestWrongMethod(t *testing.T) {
	server := httptest.NewServer(webhook.NewController(&okBuildConfigGetter{}, &okBuildConfigInstantiator{},
		map[string]webhook.Plugin{"github": New()}))
	defer server.Close()

	resp, _ := http.Get(server.URL + "/build100/secret101/github")
	body, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest ||
		!strings.Contains(string(body), "method") {
		t.Errorf("Expected BadRequest , got %s: %s!", resp.Status, string(body))
	}
}

func TestWrongContentType(t *testing.T) {
	server := httptest.NewServer(webhook.NewController(&okBuildConfigGetter{}, &okBuildConfigInstantiator{},
		map[string]webhook.Plugin{"github": New()}))
	defer server.Close()

	client := &http.Client{}
	req, _ := http.NewRequest("POST", server.URL+"/build100/secret101/github", nil)
	req.Header.Add("Content-Type", "application/text")
	req.Header.Add("User-Agent", "GitHub-Hookshot/github")
	req.Header.Add("X-Github-Event", "ping")
	resp, _ := client.Do(req)
	body, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest ||
		!strings.Contains(string(body), "Content-Type") {
		t.Errorf("Excepcted BadRequest, got %s: %s!", resp.Status, string(body))
	}
}

func TestWrongUserAgent(t *testing.T) {
	server := httptest.NewServer(webhook.NewController(&okBuildConfigGetter{}, &okBuildConfigInstantiator{},
		map[string]webhook.Plugin{"github": New()}))
	defer server.Close()

	client := &http.Client{}
	req, _ := http.NewRequest("POST", server.URL+"/build100/secret101/github", nil)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("User-Agent", "go-lang")
	req.Header.Add("X-Github-Event", "ping")
	resp, _ := client.Do(req)
	body, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest ||
		!strings.Contains(string(body), "User-Agent go-lang") {
		t.Errorf("Excepcted BadRequest, got %s: %s!", resp.Status, string(body))
	}
}

func TestMissingGithubEvent(t *testing.T) {
	server := httptest.NewServer(webhook.NewController(&okBuildConfigGetter{}, &okBuildConfigInstantiator{},
		map[string]webhook.Plugin{"github": New()}))
	defer server.Close()

	client := &http.Client{}
	req, _ := http.NewRequest("POST", server.URL+"/build100/secret101/github", nil)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("User-Agent", "GitHub-Hookshot/github")
	resp, _ := client.Do(req)
	body, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest ||
		!strings.Contains(string(body), "X-GitHub-Event") {
		t.Errorf("Excepcted BadRequest, got %s: %s!", resp.Status, string(body))
	}
}

func TestWrongGithubEvent(t *testing.T) {
	server := httptest.NewServer(webhook.NewController(&okBuildConfigGetter{}, &okBuildConfigInstantiator{},
		map[string]webhook.Plugin{"github": New()}))
	defer server.Close()

	client := &http.Client{}
	req, _ := http.NewRequest("POST", server.URL+"/build100/secret101/github", nil)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("User-Agent", "GitHub-Hookshot/github")
	req.Header.Add("X-GitHub-Event", "wrong")
	resp, _ := client.Do(req)
	body, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest ||
		!strings.Contains(string(body), "Unknown") {
		t.Errorf("Excepcted BadRequest, got %s: %s!", resp.Status, string(body))
	}
}

func TestJsonPingEvent(t *testing.T) {
	server := httptest.NewServer(webhook.NewController(&okBuildConfigGetter{}, &okBuildConfigInstantiator{},
		map[string]webhook.Plugin{"github": New()}))
	defer server.Close()

	postFile("ping", "pingevent.json", server.URL+"/build100/secret101/github",
		http.StatusOK, t)
}

func TestJsonPushEventError(t *testing.T) {
	server := httptest.NewServer(webhook.NewController(&okBuildConfigGetter{}, &okBuildConfigInstantiator{},
		map[string]webhook.Plugin{"github": New()}))
	defer server.Close()

	post("push", []byte{}, server.URL+"/build100/secret101/github", http.StatusBadRequest, t)
}

func TestJsonPushEvent(t *testing.T) {
	server := httptest.NewServer(webhook.NewController(&okBuildConfigGetter{}, &okBuildConfigInstantiator{},
		map[string]webhook.Plugin{"github": New()}))
	defer server.Close()

	postFile("push", "pushevent.json", server.URL+"/build100/secret101/github",
		http.StatusOK, t)
}

func postFile(event, filename, url string, expStatusCode int, t *testing.T) {
	data, err := ioutil.ReadFile("fixtures/" + filename)
	if err != nil {
		t.Errorf("Failed to open %s: %v", filename, err)
	}

	post(event, data, url, expStatusCode, t)
}

func post(event string, data []byte, url string, expStatusCode int, t *testing.T) {
	client := &http.Client{}
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		t.Errorf("Error creating POST request: %v!", err)
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("User-Agent", "GitHub-Hookshot/github")
	req.Header.Add("X-Github-Event", event)
	resp, err := client.Do(req)

	if err != nil {
		t.Errorf("Failed posting webhook to: %s!", url)
	}
	body, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != expStatusCode {
		t.Errorf("Wrong response code, expecting %d, got %s: %s!",
			expStatusCode, resp.Status, string(body))
	}
}

type testContext struct {
	plugin   WebHook
	buildCfg *api.BuildConfig
	req      *http.Request
	path     string
}

func setup(t *testing.T, filename, eventType string) *testContext {
	context := testContext{
		plugin: WebHook{},
		buildCfg: &api.BuildConfig{
			Triggers: []api.BuildTriggerPolicy{
				{
					Type: api.GithubWebHookBuildTriggerType,
					GithubWebHook: &api.WebHookTrigger{
						Secret: "secret101",
					},
				},
			},
			Parameters: api.BuildParameters{
				Source: api.BuildSource{
					Type: api.BuildSourceGit,
					Git: &api.GitBuildSource{
						URI: "git://github.com/my/repo.git",
					},
				},
				Strategy: mockBuildStrategy,
			},
		},
		path: "/foobar",
	}
	event, err := ioutil.ReadFile("fixtures/" + filename)
	if err != nil {
		t.Errorf("Failed to open %s: %v", filename, err)
	}
	req, err := http.NewRequest("POST", "http://origin.com", bytes.NewReader(event))
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("User-Agent", "GitHub-Hookshot/github")
	req.Header.Add("X-Github-Event", eventType)

	context.req = req
	return &context
}

func TestExtractForAPingEvent(t *testing.T) {
	//setup
	context := setup(t, "pingevent.json", "ping")

	//execute
	_, proceed, err := context.plugin.Extract(context.buildCfg, "secret101", context.path, context.req)

	//validation
	if err != nil {
		t.Errorf("Error while extracting build info: %s", err)
	}
	if proceed {
		t.Errorf("The 'proceed' return value should equal 'false' %t", proceed)
	}
}

func TestExtractProvidesValidBuildForAPushEvent(t *testing.T) {
	//setup
	context := setup(t, "pushevent.json", "push")

	//execute
	revision, proceed, err := context.plugin.Extract(context.buildCfg, "secret101", context.path, context.req)

	//validation
	if err != nil {
		t.Errorf("Error while extracting build info: %s", err)
	}
	if !proceed {
		t.Errorf("The 'proceed' return value should equal 'true' %t", proceed)
	}
	if revision == nil {
		t.Error("Expecting the revision to not be nil")
	} else {
		if revision.Git.Commit != "9bdc3a26ff933b32f3e558636b58aea86a69f051" {
			t.Error("Expecting the revision to contain the commit id from the push event")
		}
	}
}

func TestExtractProvidesValidBuildForAPushEventOtherThanMaster(t *testing.T) {
	//setup
	context := setup(t, "pushevent-not-master-branch.json", "push")
	context.buildCfg.Parameters.Source.Git.Ref = "my_other_branch"

	//execute
	revision, proceed, err := context.plugin.Extract(context.buildCfg, "secret101", context.path, context.req)

	//validation
	if err != nil {
		t.Errorf("Error while extracting build info: %s", err)
	}
	if !proceed {
		t.Errorf("The 'proceed' return value should equal 'true' %t", proceed)
	}
	if revision == nil {
		t.Error("Expecting the revision to not be nil")
	} else {
		if revision.Git.Commit != "9bdc3a26ff933b32f3e558636b58aea86a69f051" {
			t.Error("Expecting the revision to contain the commit id from the push event")
		}
	}
}

func TestExtractSkipsBuildForUnmatchedBranches(t *testing.T) {
	//setup
	context := setup(t, "pushevent.json", "push")
	context.buildCfg.Parameters.Source.Git.Ref = "adfj32qrafdavckeaewra"

	//execute
	_, proceed, _ := context.plugin.Extract(context.buildCfg, "secret101", context.path, context.req)
	if proceed {
		t.Errorf("Expecting to not continue from this event because the branch is not for this buildConfig '%s'", context.buildCfg.Parameters.Source.Git.Ref)
	}
}
