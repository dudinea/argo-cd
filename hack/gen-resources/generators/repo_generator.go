package generator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-cd/v3/hack/gen-resources/util"
	"github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"

	"k8s.io/client-go/kubernetes"
)

type Repo struct {
	Id   int    `json:"id"`
	Url  string `json:"html_url"` //nolint:revive //FIXME(var-naming)
	Name string `json:"name"`
}

type RepoGenerator struct {
	clientSet *kubernetes.Clientset
	bar       *util.Bar
}

func NewRepoGenerator(clientSet *kubernetes.Clientset) Generator {
	return &RepoGenerator{clientSet: clientSet, bar: &util.Bar{}}
}

func repositoryMatches(repo *v1alpha1.Repository, name string, regexStr string) (bool, error) {
	if len(regexStr) > 0 {
		regex, err := regexp.Compile(regexStr)
		if err != nil {
			return false, fmt.Errorf("filed to compile regex '%s': %v", regexStr, err)
		}
		return regex.MatchString(repo.Repo), nil
	}
	return repo.Repo == name, nil
}

func repoMatches(repo *Repo, name string, regexStr string) (bool, error) {
	if len(regexStr) > 0 {
		regex, err := regexp.Compile(regexStr)
		if err != nil {
			return false, fmt.Errorf("filed to compile regex '%s': %v", regexStr, err)
		}
		return regex.MatchString(repo.Name), nil
	}
	return repo.Name == name, nil
}

func filterRepos(repos []Repo, name string, regexp string) ([]Repo, error) {
	filteredRepos := []Repo{}
	for _, repo := range repos {
		matches, err := repoMatches(&repo, name, regexp)
		if err != nil {
			return nil, err
		}
		if matches {
			filteredRepos = append(filteredRepos, repo)
			log.Printf("matched repo '%s'", repo.Name)
		}
	}
	return filteredRepos, nil
}

func filterRepositories(repos []*v1alpha1.Repository, name string, regexp string) ([]v1alpha1.Repository, error) {
	filteredRepos := []v1alpha1.Repository{}
	for _, repo := range repos {
		log.Printf("matching repo %v to spec name='%s' regexp='%s'", *repo, name, regexp)
		matches, err := repositoryMatches(repo, name, regexp)
		if err != nil {
			return nil, err
		}
		if matches {
			filteredRepos = append(filteredRepos, *repo)
			log.Printf("matched repo '%s'", repo.Name)
		} else {
			log.Printf("not matched repo '%s'", repo.Name)
		}
	}
	return filteredRepos, nil
}

func fetchRepos(ctx context.Context, token string, page int) ([]Repo, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://api.github.com/repos/argoproj/argocd-example-apps/forks?per_page=100&page=%v", page), http.NoBody)
	req.Header.Set("Authorization", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var repos []Repo
	err = json.Unmarshal(body, &repos)
	if err != nil {
		return nil, errors.New("failed to retrieve repos, reason: " + string(body))
	}
	return repos, nil
}

func FetchRepos(token string, samples int) ([]Repo, error) {
	log.Print("Fetch repos started")
	var (
		ctx   = context.Background()
		repos []Repo
		page  = 1
	)

	for {
		if page%10 == 0 {
			log.Printf("Fetch repos, page: %v", page)
		}
		fetchedRepos, err := fetchRepos(ctx, token, page)
		if err != nil {
			return nil, err
		}
		if len(fetchedRepos) == 0 {
			break
		}
		if len(repos)+len(fetchedRepos) > samples {
			repos = append(repos, fetchedRepos[0:samples-len(repos)]...)
			break
		}
		repos = append(repos, fetchedRepos...)
		page++
	}
	return repos, nil
}

func (rg *RepoGenerator) Generate(opts *util.GenerateOpts) error {
	err := rg.GenerateFixedRepos(opts)
	return err
}

func fetchFixedRepos(ctx context.Context, token string, spec util.FixedRepo) ([]Repo, error) {
	//var repoSourceType string
	var repoSource string
	extraArgs := ""
	if len(spec.User) > 0 {
		//repoSourceType = "orgs"
		repoSource = "users/" + spec.User
	} else if len(spec.Org) > 0 {
		//repoSourceType = "users"
		repoSource = "orgs/" + spec.Org
	} else {
		repoSource = "user"
		extraArgs = "&affiliation=owner,collaborator,organization_member"
	}

	reqTemplate := "https://api.github.com/%s/repos?visibility=all&per_page=%d&page=%d%s"
	page := 1
	perPage := 30
	var repos []Repo
	for {
		log.Printf("retrieving page %d", page)
		reqUrl := fmt.Sprintf(reqTemplate, repoSource, perPage, page, extraArgs)
		log.Printf("req url is '%s'", reqUrl)

		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, reqUrl, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		log.Printf("using auth token '%s'", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		var allRepos []Repo
		err = json.Unmarshal(body, &allRepos)
		if err != nil {
			return nil, errors.New("failed to retrieve fixedRepos, reason: " + string(body))
		}
		log.Printf("Fetched %d repos from %s/%s page %d", len(allRepos), repoSource, spec.Name, page)
		if len(allRepos) == 0 {
			// FIXME: use links keader
			break
		}

		filteredRepos, err := filterRepos(allRepos, spec.Name, spec.Regex)
		if err != nil {
			return nil, err
		}
		repos = append(repos, filteredRepos...)
		// for _, repo := range allRepos {
		// 	log.Printf("found repo '%s'", repo.Name)
		// 	matches, err := repoMatches(&repo, &spec)
		// 	if err != nil {
		// 		return nil, err
		// 	}
		// 	if matches {
		// 		repos = append(repos, repo)
		// 		log.Printf("matched repo '%s'", repo.Name)
		// 	}
		// }

		page++
	}
	return repos, nil
}

func FetchFixedRepos(token string, fixedRepos util.FixedRepos) ([]Repo, error) {
	log.Print("Fetch fixed repos started")
	var (
		ctx   = context.Background()
		repos []Repo
		//page  = 1
	)
	samples := fixedRepos.Samples
	for specIdx, repoSpec := range fixedRepos.FixedReposList {
		log.Printf("* fetching repos for spec %d %v", specIdx, repoSpec)
		fetchedRepos, err := fetchFixedRepos(ctx, token, repoSpec)
		if err != nil {
			return nil, err
		}
		numFetched := len(fetchedRepos)
		log.Printf("Fetched %d repos for %s", numFetched, repoSpec.Name)
		if numFetched == 0 {
			continue
		}
		if len(fetchedRepos) > samples {
			repos = append(repos, fetchedRepos[0:samples]...)
			continue
		}
		repos = append(repos, fetchedRepos...)
	}
	return repos, nil
}

func (rg *RepoGenerator) GenerateFixedRepos(opts *util.GenerateOpts) error {
	repos, err := FetchFixedRepos(opts.GithubToken, opts.RepositoryOpts.FixedRepos)
	if err != nil {
		return err
	}
	return rg.MakeRepoSecrets(repos, opts)
}

func (rg *RepoGenerator) MakeRepoSecrets(repos []Repo, opts *util.GenerateOpts) error {
	var err error
	secrets := rg.clientSet.CoreV1().Secrets(opts.Namespace)
	rg.bar.NewOption(0, int64(len(repos)))
	for _, repo := range repos {
		_, err = secrets.Create(context.TODO(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "repo-",
				Namespace:    opts.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/generated-by": "argocd-generator",
					"argocd.argoproj.io/secret-type": "repository",
				},
				Annotations: map[string]string{
					"managed-by": "argocd.argoproj.io",
				},
			},
			Data: map[string][]byte{
				"type":    []byte("git"),
				"url":     []byte(repo.Url),
				"project": []byte("default"),
			},
		}, metav1.CreateOptions{})
		rg.bar.Increment()
		rg.bar.Play()
	}
	rg.bar.Finish()
	if err != nil {
		return err
	}
	return nil
}

func (rg *RepoGenerator) GenerateFromForks(opts *util.GenerateOpts) error {
	repos, err := FetchRepos(opts.GithubToken, opts.RepositoryOpts.Samples)
	if err != nil {
		return err
	}

	secrets := rg.clientSet.CoreV1().Secrets(opts.Namespace)
	rg.bar.NewOption(0, int64(len(repos)))
	for _, repo := range repos {
		_, err = secrets.Create(context.TODO(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "repo-",
				Namespace:    opts.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/generated-by": "argocd-generator",
					"argocd.argoproj.io/secret-type": "repository",
				},
				Annotations: map[string]string{
					"managed-by": "argocd.argoproj.io",
				},
			},
			Data: map[string][]byte{
				"type":    []byte("git"),
				"url":     []byte(repo.Url),
				"project": []byte("default"),
			},
		}, metav1.CreateOptions{})
		rg.bar.Increment()
		rg.bar.Play()
	}
	rg.bar.Finish()
	if err != nil {
		return err
	}
	return nil
}

func (rg *RepoGenerator) Clean(opts *util.GenerateOpts) error {
	log.Printf("Clean repos")
	secrets := rg.clientSet.CoreV1().Secrets(opts.Namespace)
	return secrets.DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/generated-by=argocd-generator",
	})
}
