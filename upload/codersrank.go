package upload

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	config "github.com/codersrank-org/multi_repo_repo_extractor/config"
	"github.com/pkg/browser"
)

// CodersrankService uploads and merge results with codersrank
type CodersrankService interface {
	UploadRepo(repoID string) (string, error)
	UploadResults(results map[string]string) string
	ProcessResults(resultToken string)
}

type codersrankService struct {
	UploadRepoURL   string
	UploadResultURL string
	ProcessURL      string
	AppPath         string
}

// NewCodersrankService constructor
func NewCodersrankService(c config.Config) CodersrankService {
	return &codersrankService{
		UploadRepoURL:   "https://grpcgateway.codersrank.io/candidate/privaterepo/Upload",
		UploadResultURL: "https://grpcgateway.codersrank.io/multi/repo/results",
		ProcessURL:      "https://profile.codersrank.io/repo?multiToken=",
		AppPath:         c.AppPath,
	}
}

func (c *codersrankService) UploadRepo(repoID string) (string, error) {

	// Read file
	filename := fmt.Sprintf("%s/%s.zip", c.getSaveResultPath(), repoID)
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Add file as multipart/form-data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(file.Name()))
	if err != nil {
		return "", err
	}
	io.Copy(part, file)
	writer.Close()

	// Create and make the request
	request, err := http.NewRequest("POST", c.UploadRepoURL, body)
	if err != nil {
		return "", err
	}
	request.Header.Add("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", errors.New("Server returned non 200 response")
	}
	defer response.Body.Close()

	content, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	// Get response and return resulting token
	var result CRUploadResult
	err = json.Unmarshal(content, &result)
	if err != nil {
		return "", err
	}

	return result.Token, nil
}

func (c *codersrankService) UploadResults(results map[string]string) string {

	multiUpload := MultiUpload{}
	multiUpload.Results = make([]CRUploadResultWithRepoName, len(results))

	i := 0
	for reponame, token := range results {
		multiUpload.Results[i] = CRUploadResultWithRepoName{
			Token:    token,
			Reponame: reponame,
		}
		i++
	}

	b, err := json.Marshal(multiUpload)
	if err != nil {
		log.Fatal(err)
	}
	req, err := http.NewRequest("POST", c.UploadResultURL, bytes.NewBuffer(b))
	req.Header.Set("X-Custom-Header", "myvalue")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	var result CRUploadResult
	err = json.Unmarshal(body, &result)
	if err != nil {
		log.Fatal(err)
	}

	return result.Token

}

func (c *codersrankService) ProcessResults(resultToken string) {
	browserURL := c.ProcessURL + resultToken
	browser.OpenURL(browserURL)
}

func (c *codersrankService) getSaveResultPath() string {
	resultPath := c.AppPath + "/results"
	if _, err := os.Stat(resultPath); os.IsNotExist(err) {
		os.Mkdir(resultPath, 0700)
	}
	return resultPath
}

// CRUploadResult is the result of single repo upload
type CRUploadResult struct {
	Token string `json:"token"`
}

// MultiUpload is the request body
type MultiUpload struct {
	Results []CRUploadResultWithRepoName `json:"results"`
}

// CRUploadResultWithRepoName token-reponame pair
type CRUploadResultWithRepoName struct {
	Token    string `json:"token"`
	Reponame string `json:"reponame"`
}
