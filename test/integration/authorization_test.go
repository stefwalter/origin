// +build integration,!no-etcd

package integration

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	kapierror "github.com/GoogleCloudPlatform/kubernetes/pkg/api/errors"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/fields"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	testutil "github.com/openshift/origin/test/util"

	authorizationapi "github.com/openshift/origin/pkg/authorization/api"
	"github.com/openshift/origin/pkg/client"
	policy "github.com/openshift/origin/pkg/cmd/admin/policy"
	"github.com/openshift/origin/pkg/cmd/server/bootstrappolicy"
)

func TestRestrictedAccessForProjectAdmins(t *testing.T) {
	_, clusterAdminKubeConfig, err := testutil.StartTestMaster()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clusterAdminClient, err := testutil.GetClusterAdminClient(clusterAdminKubeConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clusterAdminClientConfig, err := testutil.GetClusterAdminClientConfig(clusterAdminKubeConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	haroldClient, err := testutil.CreateNewProject(clusterAdminClient, *clusterAdminClientConfig, "hammer-project", "harold")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	markClient, err := testutil.CreateNewProject(clusterAdminClient, *clusterAdminClientConfig, "mallet-project", "mark")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = haroldClient.Deployments("hammer-project").List(labels.Everything(), fields.Everything())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = markClient.Deployments("hammer-project").List(labels.Everything(), fields.Everything())
	if (err == nil) || !kapierror.IsForbidden(err) {
		t.Fatalf("unexpected error: %v", err)
	}

	// projects are a special case where a get of a project actually sets a namespace.  Make sure that
	// the namespace is properly special cased and set for authorization rules
	_, err = haroldClient.Projects().Get("hammer-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = markClient.Projects().Get("hammer-project")
	if (err == nil) || !kapierror.IsForbidden(err) {
		t.Fatalf("unexpected error: %v", err)
	}

	// wait for the project authorization cache to catch the change.  It is on a one second period
	waitForProject(t, haroldClient, "hammer-project", 5*time.Second, 10)
	waitForProject(t, markClient, "mallet-project", 5*time.Second, 10)
}

// waitForProject will execute a client list of projects looking for the project with specified name
// if not found, it will retry up to numRetries at the specified delayInterval
func waitForProject(t *testing.T, client client.Interface, projectName string, delayInterval time.Duration, numRetries int) {
	for i := 0; i <= numRetries; i++ {
		projects, err := client.Projects().List(labels.Everything(), fields.Everything())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if (len(projects.Items) == 1) && (projects.Items[0].Name == projectName) {
			fmt.Printf("Waited %v times with interval %v\n", i, delayInterval)
			return
		} else {
			time.Sleep(delayInterval)
		}
	}
	t.Errorf("expected project %v not found", projectName)
}

func TestOnlyResolveRolesForBindingsThatMatter(t *testing.T) {
	_, clusterAdminKubeConfig, err := testutil.StartTestMaster()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clusterAdminClient, err := testutil.GetClusterAdminClient(clusterAdminKubeConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	addValerie := &policy.RoleModificationOptions{
		RoleNamespace:    bootstrappolicy.DefaultMasterAuthorizationNamespace,
		RoleName:         bootstrappolicy.ViewRoleName,
		BindingNamespace: bootstrappolicy.DefaultMasterAuthorizationNamespace,
		Client:           clusterAdminClient,
		Users:            []string{"valerie"},
	}
	if err := addValerie.AddRole(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err = clusterAdminClient.Roles(bootstrappolicy.DefaultMasterAuthorizationNamespace).Delete(bootstrappolicy.ViewRoleName); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	addEdgar := &policy.RoleModificationOptions{
		RoleNamespace:    bootstrappolicy.DefaultMasterAuthorizationNamespace,
		RoleName:         bootstrappolicy.EditRoleName,
		BindingNamespace: bootstrappolicy.DefaultMasterAuthorizationNamespace,
		Client:           clusterAdminClient,
		Users:            []string{"edgar"},
	}
	if err := addEdgar.AddRole(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// try to add Valerie to a non-existent role
	if err := addValerie.AddRole(); !kapierror.IsNotFound(err) {
		t.Fatalf("unexpected error: %v", err)
	}

}

// TODO this list should start collapsing as we continue to tighten access on generated system ids
var globalClusterAdminUsers = util.NewStringSet("system:kube-client", "system:openshift-client", "system:openshift-deployer")
var globalClusterAdminGroups = util.NewStringSet("system:cluster-admins", "system:nodes")

type resourceAccessReviewTest struct {
	clientInterface client.ResourceAccessReviewInterface
	review          *authorizationapi.ResourceAccessReview

	response authorizationapi.ResourceAccessReviewResponse
	err      string
}

func (test resourceAccessReviewTest) run(t *testing.T) {
	actualResponse, err := test.clientInterface.Create(test.review)
	if len(test.err) > 0 {
		if err == nil {
			t.Errorf("Expected error: %v", test.err)
		} else if !strings.Contains(err.Error(), test.err) {
			t.Errorf("expected %v, got %v", test.err, err)
		}
	} else {
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}

	if reflect.DeepEqual(actualResponse, test.response) {
		t.Errorf("%#v: expected %v, got %v", test.review, test.response, actualResponse)
	}
}

func TestResourceAccessReview(t *testing.T) {
	_, clusterAdminKubeConfig, err := testutil.StartTestMaster()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clusterAdminClient, err := testutil.GetClusterAdminClient(clusterAdminKubeConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clusterAdminClientConfig, err := testutil.GetClusterAdminClientConfig(clusterAdminKubeConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	haroldClient, err := testutil.CreateNewProject(clusterAdminClient, *clusterAdminClientConfig, "hammer-project", "harold")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	markClient, err := testutil.CreateNewProject(clusterAdminClient, *clusterAdminClientConfig, "mallet-project", "mark")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	addValerie := &policy.RoleModificationOptions{
		RoleNamespace:    bootstrappolicy.DefaultMasterAuthorizationNamespace,
		RoleName:         bootstrappolicy.ViewRoleName,
		BindingNamespace: "hammer-project",
		Client:           haroldClient,
		Users:            []string{"valerie"},
	}
	if err := addValerie.AddRole(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	addEdgar := &policy.RoleModificationOptions{
		RoleNamespace:    bootstrappolicy.DefaultMasterAuthorizationNamespace,
		RoleName:         bootstrappolicy.EditRoleName,
		BindingNamespace: "mallet-project",
		Client:           markClient,
		Users:            []string{"edgar"},
	}
	if err := addEdgar.AddRole(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	requestWhoCanViewDeployments := &authorizationapi.ResourceAccessReview{Verb: "get", Resource: "deployments"}

	{
		test := resourceAccessReviewTest{
			clientInterface: haroldClient.ResourceAccessReviews("hammer-project"),
			review:          requestWhoCanViewDeployments,
			response: authorizationapi.ResourceAccessReviewResponse{
				Users:     util.NewStringSet("harold", "valerie"),
				Groups:    globalClusterAdminGroups,
				Namespace: "hammer-project",
			},
		}
		test.response.Users.Insert(globalClusterAdminUsers.List()...)
		test.run(t)
	}
	{
		test := resourceAccessReviewTest{
			clientInterface: markClient.ResourceAccessReviews("mallet-project"),
			review:          requestWhoCanViewDeployments,
			response: authorizationapi.ResourceAccessReviewResponse{
				Users:     util.NewStringSet("mark", "edgar"),
				Groups:    globalClusterAdminGroups,
				Namespace: "mallet-project",
			},
		}
		test.response.Users.Insert(globalClusterAdminUsers.List()...)
		test.run(t)
	}

	// mark should not be able to make global access review requests
	{
		test := resourceAccessReviewTest{
			clientInterface: markClient.ClusterResourceAccessReviews(),
			review:          requestWhoCanViewDeployments,
			err:             "forbidden",
		}
		test.run(t)
	}

	// a cluster-admin should be able to make global access review requests
	{
		test := resourceAccessReviewTest{
			clientInterface: clusterAdminClient.ClusterResourceAccessReviews(),
			review:          requestWhoCanViewDeployments,
			response: authorizationapi.ResourceAccessReviewResponse{
				Users:  globalClusterAdminUsers,
				Groups: globalClusterAdminGroups,
			},
		}
		test.run(t)
	}
}

type subjectAccessReviewTest struct {
	clientInterface client.SubjectAccessReviewInterface
	review          *authorizationapi.SubjectAccessReview

	response authorizationapi.SubjectAccessReviewResponse
	err      string
}

func (test subjectAccessReviewTest) run(t *testing.T) {
	actualResponse, err := test.clientInterface.Create(test.review)
	if len(test.err) > 0 {
		if err == nil {
			t.Errorf("Expected error: %v", test.err)
		} else if !strings.Contains(err.Error(), test.err) {
			t.Errorf("expected %v, got %v", test.err, err)
		}
	} else {
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}

	if reflect.DeepEqual(actualResponse, test.response) {
		t.Errorf("%#v: expected %v, got %v", test.review, test.response, actualResponse)
	}
}

func TestSubjectAccessReview(t *testing.T) {
	_, clusterAdminKubeConfig, err := testutil.StartTestMaster()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clusterAdminClient, err := testutil.GetClusterAdminClient(clusterAdminKubeConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clusterAdminClientConfig, err := testutil.GetClusterAdminClientConfig(clusterAdminKubeConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	haroldClient, err := testutil.CreateNewProject(clusterAdminClient, *clusterAdminClientConfig, "hammer-project", "harold")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	markClient, err := testutil.CreateNewProject(clusterAdminClient, *clusterAdminClientConfig, "mallet-project", "mark")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	addValerie := &policy.RoleModificationOptions{
		RoleNamespace:    bootstrappolicy.DefaultMasterAuthorizationNamespace,
		RoleName:         bootstrappolicy.ViewRoleName,
		BindingNamespace: "hammer-project",
		Client:           haroldClient,
		Users:            []string{"valerie"},
	}
	if err := addValerie.AddRole(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	addEdgar := &policy.RoleModificationOptions{
		RoleNamespace:    bootstrappolicy.DefaultMasterAuthorizationNamespace,
		RoleName:         bootstrappolicy.EditRoleName,
		BindingNamespace: "mallet-project",
		Client:           markClient,
		Users:            []string{"edgar"},
	}
	if err := addEdgar.AddRole(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	askCanValerieGetProject := &authorizationapi.SubjectAccessReview{User: "valerie", Verb: "get", Resource: "projects"}
	subjectAccessReviewTest{
		clientInterface: haroldClient.SubjectAccessReviews("hammer-project"),
		review:          askCanValerieGetProject,
		response: authorizationapi.SubjectAccessReviewResponse{
			Allowed:   true,
			Reason:    "allowed by rule in hammer-project",
			Namespace: "hammer-project",
		},
	}.run(t)
	subjectAccessReviewTest{
		clientInterface: markClient.SubjectAccessReviews("mallet-project"),
		review:          askCanValerieGetProject,
		response: authorizationapi.SubjectAccessReviewResponse{
			Allowed:   false,
			Reason:    "denied by default",
			Namespace: "mallet-project",
		},
	}.run(t)

	askCanEdgarDeletePods := &authorizationapi.SubjectAccessReview{User: "edgar", Verb: "delete", Resource: "pods"}
	subjectAccessReviewTest{
		clientInterface: markClient.SubjectAccessReviews("mallet-project"),
		review:          askCanEdgarDeletePods,
		response: authorizationapi.SubjectAccessReviewResponse{
			Allowed:   true,
			Reason:    "allowed by rule in mallet-project",
			Namespace: "mallet-project",
		},
	}.run(t)
	subjectAccessReviewTest{
		clientInterface: haroldClient.SubjectAccessReviews("mallet-project"),
		review:          askCanEdgarDeletePods,
		err:             "forbidden",
	}.run(t)

	askCanHaroldUpdateProject := &authorizationapi.SubjectAccessReview{User: "harold", Verb: "update", Resource: "projects"}
	subjectAccessReviewTest{
		clientInterface: haroldClient.SubjectAccessReviews("hammer-project"),
		review:          askCanHaroldUpdateProject,
		response: authorizationapi.SubjectAccessReviewResponse{
			Allowed:   true,
			Reason:    "allowed by rule in hammer-project",
			Namespace: "hammer-project",
		},
	}.run(t)

	askCanClusterAdminsCreateProject := &authorizationapi.SubjectAccessReview{Groups: util.NewStringSet("system:cluster-admins"), Verb: "create", Resource: "projects"}
	subjectAccessReviewTest{
		clientInterface: clusterAdminClient.ClusterSubjectAccessReviews(),
		review:          askCanClusterAdminsCreateProject,
		response: authorizationapi.SubjectAccessReviewResponse{
			Allowed:   true,
			Reason:    "",
			Namespace: "",
		},
	}.run(t)
	subjectAccessReviewTest{
		clientInterface: haroldClient.ClusterSubjectAccessReviews(),
		review:          askCanClusterAdminsCreateProject,
		err:             "forbidden",
	}.run(t)

	askCanICreatePods := &authorizationapi.SubjectAccessReview{Verb: "create", Resource: "projects"}
	subjectAccessReviewTest{
		clientInterface: haroldClient.SubjectAccessReviews("hammer-project"),
		review:          askCanICreatePods,
		response: authorizationapi.SubjectAccessReviewResponse{
			Allowed:   true,
			Reason:    "allowed by rule in hammer-project",
			Namespace: "hammer-project",
		},
	}.run(t)
	askCanICreatePolicyBindings := &authorizationapi.SubjectAccessReview{Verb: "create", Resource: "policybindings"}
	subjectAccessReviewTest{
		clientInterface: haroldClient.SubjectAccessReviews("hammer-project"),
		review:          askCanICreatePolicyBindings,
		response: authorizationapi.SubjectAccessReviewResponse{
			Allowed:   false,
			Reason:    "denied by default",
			Namespace: "hammer-project",
		},
	}.run(t)

}
