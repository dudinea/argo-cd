package generator

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/argoproj/argo-cd/v3/hack/gen-resources/util"
	"github.com/argoproj/argo-cd/v3/util/settings"

	"github.com/argoproj/argo-cd/v3/util/db"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	appclientset "github.com/argoproj/argo-cd/v3/pkg/client/clientset/versioned"
	appclientsetv1alpha1 "github.com/argoproj/argo-cd/v3/pkg/client/clientset/versioned/typed/application/v1alpha1"
)

var seed = rand.New(rand.NewSource(time.Now().UnixNano()))

type ApplicationGenerator struct {
	argoClientSet *appclientset.Clientset
	clientSet     *kubernetes.Clientset
}

func NewApplicationGenerator(argoClientSet *appclientset.Clientset, clientSet *kubernetes.Clientset) Generator {
	return &ApplicationGenerator{argoClientSet, clientSet}
}

func (generator *ApplicationGenerator) buildRandomSource(repositories []*v1alpha1.Repository) (*v1alpha1.ApplicationSource, error) {
	repoNumber := seed.Int() % len(repositories)
	return &v1alpha1.ApplicationSource{
		RepoURL:        repositories[repoNumber].Repo,
		Path:           "helm-guestbook",
		TargetRevision: "master",
	}, nil
}

func (generator *ApplicationGenerator) buildSource(sourcespec *util.SourceOpts, repositories []*v1alpha1.Repository) (v1alpha1.ApplicationSource, error) {
	/*if opts.ApplicationOpts.SourceOpts.Strategy == "Random" {
		return generator.buildRandomSource(repositories)
	}*/
	//return generator.buildRandomSource(repositories)
	//log.Printf("selecting from repos: %v", repositories)
	filteredRepos, err := filterRepositories(repositories, "", sourcespec.RepoRegex)
	if err != nil {
		return v1alpha1.ApplicationSource{}, fmt.Errorf("Failed to filter repos for source spec %v: %v", sourcespec, err)
	}
	if len(filteredRepos) == 0 {
		return v1alpha1.ApplicationSource{}, fmt.Errorf("Failed to find matching repo for source spec %v", sourcespec)
	}
	repoNumber := seed.Int() % len(filteredRepos)
	return v1alpha1.ApplicationSource{
		RepoURL:        filteredRepos[repoNumber].Repo,
		Path:           sourcespec.Path,
		TargetRevision: sourcespec.TargetRevision,
	}, nil
}

func (generator *ApplicationGenerator) buildRandomDestination(opts util.ApplicationOpts, clusters []v1alpha1.Cluster) (*v1alpha1.ApplicationDestination, error) {
	namespace := opts.DestinationOpts.Namespace
	if "" == namespace {
		namespace = opts.GeneratedName
	}
	clusterNumber := seed.Int() % len(clusters)
	return &v1alpha1.ApplicationDestination{
		Namespace: namespace,
		Name:      clusters[clusterNumber].Name,
	}, nil
}

func (generator *ApplicationGenerator) buildDestination(opts util.ApplicationOpts, clusters []v1alpha1.Cluster) (*v1alpha1.ApplicationDestination, error) {
	// FIXME
	/*if opts.ApplicationOpts.DestinationOpts.Strategy == "Random" {
		return generator.buildRandomDestination(opts, clusters)
	}*/
	return generator.buildRandomDestination(opts, clusters)
}

func (generator *ApplicationGenerator) Generate(opts *util.GenerateOpts) error {
	settingsMgr := settings.NewSettingsManager(context.TODO(), generator.clientSet, opts.Namespace)
	repositories, err := db.NewDB(opts.Namespace, settingsMgr, generator.clientSet).ListRepositories(context.TODO())
	if err != nil {
		return err
	}
	clusters, err := db.NewDB(opts.Namespace, settingsMgr, generator.clientSet).ListClusters(context.TODO())
	if err != nil {
		return err
	}
	applications := generator.argoClientSet.ArgoprojV1alpha1().Applications(opts.Namespace)
	for i := 0; i < len(opts.ApplicationsOpts); i++ {
		log.Printf("generating applications for spec %d", i)
		err := generator.GenerateApplicationsForSpec(opts.ApplicationsOpts[i], repositories, clusters, applications)
		if err != nil {
			return fmt.Errorf("Failed to generate applications from spec #%d: %v", i, err)
		}
	}
	return nil
}

func (generator *ApplicationGenerator) GenerateApplicationsForSpec(spec util.ApplicationOpts,
	repositories []*v1alpha1.Repository,
	clusters *v1alpha1.ClusterList,
	applications appclientsetv1alpha1.ApplicationInterface) error {
	name := spec.Name
	for i := 0; i < spec.Samples; i++ {
		spec.GeneratedName = name + "-" + util.GetRandomString()[0:5]
		log.Printf("Generate application %s#%v -> %s", name, i, spec.GeneratedName)
		var srcPtr *v1alpha1.ApplicationSource
		sources := []v1alpha1.ApplicationSource{}
		if len(spec.SourcesOpts) > 0 {
			for j := 0; j < len(spec.SourcesOpts); j++ {
				tmpSrc, err := generator.buildSource(&spec.SourcesOpts[j], repositories)
				if err != nil {
					return err
				}
				log.Printf("Pick source %q", tmpSrc)
				sources = append(sources, tmpSrc)
			}
		} else {
			source, err := generator.buildSource(&spec.SourceOpts, repositories)
			if err != nil {
				return err
			}
			log.Printf("Pick source %q", source)
			srcPtr = &source
		}

		destination, err := generator.buildDestination(spec, clusters.Items)
		if err != nil {
			return err
		}
		log.Printf("Pick destination %q", destination)
		log.Printf("Create application")
		isAutomated := true
		syncPolicy := v1alpha1.SyncPolicy{
			SyncOptions: []string{
				"ServerSideApply=true",
				"CreateNamespace=true",
			},
			Automated: &v1alpha1.SyncPolicyAutomated{
				Enabled:    &isAutomated,
				SelfHeal:   true,
				Prune:      true,
				AllowEmpty: true,
			},
		}
		_, err = applications.Create(context.TODO(), &v1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				//GenerateName: name + "-",
				Name:       spec.GeneratedName,
				Namespace:  spec.Namespace,
				Labels:     labels,
				Finalizers: []string{"resources-finalizer.argocd.argoproj.io"},
			},
			Spec: v1alpha1.ApplicationSpec{
				Project:     "default",
				Destination: *destination,
				Sources:     sources,
				Source:      srcPtr,
				SyncPolicy:  &syncPolicy,
			},
		}, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (generator *ApplicationGenerator) Clean(opts *util.GenerateOpts) error {
	log.Printf("Clean applications")
	applications := generator.argoClientSet.ArgoprojV1alpha1().Applications(opts.Namespace)
	return applications.DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/generated-by=argocd-generator",
	})
}
