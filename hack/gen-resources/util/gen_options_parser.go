package util

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

type SourceOpts struct {
	RepoRegex      string `yaml:"repoRegex"`
	TargetRevision string `yaml:"targetRevision"`
	Path           string `yaml:"path"`
	Strategy       string `yaml:"strategy"`
}

type DestinationOpts struct {
	Strategy  string `yaml:"strategy"`
	Namespace string `yaml:"namespace"`
}

type ApplicationOpts struct {
	Name            string          `yaml:"name"`
	Namespace       string          `yaml:"namespace"`
	Samples         int             `yaml:"samples"`
	SourceOpts      SourceOpts      `yaml:"source"`
	SourcesOpts     []SourceOpts    `yaml:"sources"`
	DestinationOpts DestinationOpts `yaml:"destination"`
	GeneratedName   string
}

type RepositoryOpts struct {
	Samples    int        `yaml:"samples"`
	FixedRepos FixedRepos `yaml:"fixedRepos"`
}

type FixedRepos struct {
	FixedReposList []FixedRepo `yaml:"fixedReposList"`
	Samples        int         `yaml:"samples"`
}

type FixedRepo struct {
	Name  string `yaml:"name"`
	Org   string `yaml:"org"`
	User  string `yaml:"user"`
	Regex string `yaml:"regex"`
}

type ProjectOpts struct {
	Samples int `yaml:"samples"`
}

type ClusterOpts struct {
	Samples              int    `yaml:"samples"`
	NamespacePrefix      string `yaml:"namespacePrefix"`
	ValuesFilePath       string `yaml:"valuesFilePath"`
	DestinationNamespace string `yaml:"destinationNamespace"`
	ClusterNamePrefix    string `yaml:"clusterNamePrefix"`
	Concurrency          int    `yaml:"parallel"`
}

type GenerateOpts struct {
	ApplicationsOpts []ApplicationOpts `yaml:"applications"`
	ClusterOpts      ClusterOpts       `yaml:"cluster"`
	RepositoryOpts   RepositoryOpts    `yaml:"repository"`
	ProjectOpts      ProjectOpts       `yaml:"project"`
	GithubToken      string            `yaml:"githubToken"`
	Namespace        string            `yaml:"namespace"`
}

func setDefaults(opts *GenerateOpts) {
	if opts.ClusterOpts.Concurrency == 0 {
		opts.ClusterOpts.Concurrency = 2
	}
}

func Parse(opts *GenerateOpts, file string) error {
	fp, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("error reading the template file: %s : %w", file, err)
	}

	if e := yaml.Unmarshal(fp, &opts); e != nil {
		return e
	}

	setDefaults(opts)

	return nil
}
