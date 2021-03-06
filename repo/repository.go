package repo

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/gookit/color"

	"github.com/codersrank-org/multi_repo_repo_extractor/config"
	"github.com/codersrank-org/multi_repo_repo_extractor/entity"
)

// RepositoryService handles repository operations like cloning, updating and processing repos
type RepositoryService interface {
	ProcessRepos(repos []*entity.Repository) []*entity.Repository
	GetTotalRepos() int
	GetRemainingRepos() int
	GetCurrentRepo() *entity.Repository
}

type repositoryService struct {
	RepoInfoExtractorPath string
	RepoInfoExtractorURL  string
	ProviderName          string
	RepoVisibility        string
	Username              string
	Token                 string
	Emails                []string
	HashedEmails          map[string]interface{}
	SaveRepoPath          string
	AppPath               string
	TotalRepos            int
	ProcessedRepos        int
	CurrentRepository     *entity.Repository
}

// NewRepositoryService constructor
func NewRepositoryService(c config.Config) RepositoryService {
	saveRepoPath := getSaveRepoPath(c.AppPath)
	repositoryService := &repositoryService{
		RepoInfoExtractorPath: c.RepoInfoExtractorPath,
		RepoInfoExtractorURL:  "https://github.com/codersrank-org/repo_info_extractor",
		ProviderName:          c.ProviderName,
		RepoVisibility:        c.RepoVisibility,
		Token:                 c.Token,
		Emails:                c.Emails,
		SaveRepoPath:          saveRepoPath,
		AppPath:               c.AppPath,
	}

	if c.Username == "" {
		// default username to "git"
		repositoryService.Username = "git"
	} else {
		repositoryService.Username = c.Username
	}

	hashedEmails := make(map[string]interface{}, len(c.Emails))
	for _, email := range c.Emails {
		hashedEmails[md5Hash(email)] = nil
	}
	repositoryService.HashedEmails = hashedEmails
	repositoryService.initRepoInfoExtractor()
	return repositoryService
}

func (r *repositoryService) GetTotalRepos() int {
	return r.TotalRepos
}

func (r *repositoryService) GetRemainingRepos() int {
	return r.TotalRepos - r.ProcessedRepos
}

func (r *repositoryService) GetCurrentRepo() *entity.Repository {
	return r.CurrentRepository
}

func (r *repositoryService) ProcessRepos(repos []*entity.Repository) []*entity.Repository {
	r.TotalRepos = len(repos)
	processedRepos := make([]*entity.Repository, 0, len(repos))
	for _, repo := range repos {
		r.ProcessedRepos++
		r.CurrentRepository = repo
		fmt.Printf("Extracting %s (%d/%d)\n", color.Info.Sprint(repo.Name), r.ProcessedRepos, len(repos))
		err := r.clone(repo)
		if err != nil {
			fmt.Printf("Couldn't clone repo. Error: %s\n", color.Danger.Sprint(err.Error()))
			continue
		}
		err = r.process(repo)
		if err != nil {
			fmt.Printf("Couldn't process repo. Error: %s\n", color.Danger.Sprint(err.Error()))
			continue
		}
		processedRepos = append(processedRepos, repo)
	}
	return processedRepos
}

func (r *repositoryService) initRepoInfoExtractor() {
	err := cloneRepository(r.RepoInfoExtractorURL, r.RepoInfoExtractorPath, "Repo Info Extractor")
	if err != nil {
		log.Fatalf("Couldn't clone repo_info_extractor: %s", err.Error())
	}
}

func (r *repositoryService) clone(repo *entity.Repository) error {
	repoURL := fmt.Sprintf("https://%s:%s@%s/%s", r.Username, r.Token, r.ProviderName, repo.FullName)
	repoPath := r.SaveRepoPath + "/" + repo.FullName
	err := cloneRepository(repoURL, repoPath, repo.FullName)
	return err
}

func (r *repositoryService) process(repo *entity.Repository) error {
	scriptPath := r.getScriptPath()
	repoPath := r.SaveRepoPath + "/" + repo.FullName

	// Need to chdir to execute scripts because of docker
	os.Chdir(r.RepoInfoExtractorPath)
	cmd := exec.Command(scriptPath, repoPath, "--email="+strings.Join(r.Emails, ","), "--skip_upload", "--headless")

	// We can use these to print repo_info_extractor output to the screen.
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return errors.New(stderr.String())
	}
	// Move result to results folder
	sourceLocation := r.RepoInfoExtractorPath + "/repo_data.json.zip"
	targetLocation := getSaveResultPath(r.AppPath) + "/" + repo.ID + ".zip"

	err = os.Rename(sourceLocation, targetLocation)
	if err != nil {
		return err
	}

	// Check if provided emails are present in the repo
	err = r.checkEmails(targetLocation, repo.FullName)
	return err
}

// Show user a warning if none of the provided emails found in the repository
func (r *repositoryService) checkEmails(fileLocation, reponame string) error {
	zipReader, err := zip.OpenReader(fileLocation)
	if err != nil {
		err := fmt.Errorf("Couldn't read zip file for %s", reponame)
		return err
	}
	defer zipReader.Close()
	var result repoAnalysisResult
	for _, f := range zipReader.File {
		// We are looking for .json result file
		if strings.Contains(f.Name, ".json") {
			configFile, err := f.Open()
			if err != nil {
				err := fmt.Errorf("Couldn't open zip file for %s", reponame)
				return err
			}
			jsonParser := json.NewDecoder(configFile)
			if err = jsonParser.Decode(&result); err != nil {
				err := fmt.Errorf("Couldn't parse zip file %s", reponame)
				return err
			}
			break
		}
	}
	emailExistsInResult := false
	for _, commit := range result.Commits {
		if _, ok := r.HashedEmails[commit.AuthorEmail]; ok {
			emailExistsInResult = true
			break
		}
	}
	if !emailExistsInResult {
		err := fmt.Errorf("None of the provided emails (%s) found in repo %s", strings.Join(r.Emails, ", "), reponame)
		return err
	}

	return nil
}

func md5Hash(s string) string {
	hasher := md5.New()
	hasher.Write([]byte(s))
	return hex.EncodeToString(hasher.Sum(nil))
}

// TODO handle windows (.bat files)
func (r *repositoryService) getScriptPath() string {
	return r.RepoInfoExtractorPath + "/run-docker-headless.sh"
}

// Clone repository from given url to given path
func cloneRepository(url, path, name string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_, err := git.PlainClone(path, false, &git.CloneOptions{
			URL: url,
			//Prognress: os.Stdout,
			// TODO add verbose flag to show/hide these.
		})
		if err != nil {
			return err
		}
	} else {
		// If exists, pull latest changes
		repo, err := git.PlainOpen(path)
		if err != nil {
			return err
		}
		workTree, err := repo.Worktree()
		if err != nil {
			return err
		}
		err = workTree.Pull(&git.PullOptions{RemoteName: "origin"})
		if err != nil && !strings.Contains(err.Error(), "already up-to-date") && !strings.Contains(err.Error(), "worktree contains unstaged changes") {
			return err
		}
	}
	return nil
}

func getAppPath() string {
	appPath, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return appPath
}

func getSaveRepoPath(appPath string) string {
	tmpPath := appPath + "/tmp"
	if _, err := os.Stat(tmpPath); os.IsNotExist(err) {
		os.Mkdir(tmpPath, 0700)
	}
	return tmpPath
}

func getSaveResultPath(appPath string) string {
	resultPath := appPath + "/results"
	if _, err := os.Stat(resultPath); os.IsNotExist(err) {
		os.Mkdir(resultPath, 0700)
	}
	return resultPath
}

type repoAnalysisResult struct {
	RepoName       string   `json:"repoName"`
	LocalUsernames []string `json:"localUsernames"`
	Remotes        struct {
		Origin string `json:"origin"`
	} `json:"remotes"`
	PrimaryRemoteURL string `json:"primaryRemoteUrl"`
	NumberOfBranches int    `json:"numberOfBranches"`
	NumberOfTags     int    `json:"numberOfTags"`
	Commits          []struct {
		AuthorName   string   `json:"authorName"`
		AuthorEmail  string   `json:"authorEmail"`
		CreatedAt    string   `json:"createdAt"`
		CommitHash   string   `json:"commitHash"`
		IsMerge      bool     `json:"isMerge"`
		Parents      []string `json:"parents"`
		ChangedFiles []struct {
			FileName   string `json:"fileName"`
			Language   string `json:"language"`
			Insertions int    `json:"insertions"`
			Deletions  int    `json:"deletions"`
		} `json:"changedFiles"`
		IsDuplicated bool `json:"isDuplicated"`
	} `json:"commits"`
	EmailsV2 []string `json:"emails_v2"`
}
