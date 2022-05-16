package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xanzy/go-gitlab"
)

var GitlabApiToken, ProjectId string

type Alert struct {
	Fingerprint  string            `json:"fingerprint"`
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
}

type Payload struct {
	Receiver    string  `json:"receiver"`
	Status      string  `json:"status"`
	Alerts      []Alert `json:"alerts"`
	GroupLabels struct {
		Alertname string `json:"alertname"`
		Job       string `json:"job"`
	} `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
}

type DescriptionData struct {
	Fingerprint     string
	Annotations     map[string]string
	StartsAt        string
	EndsAt          string
	AlertmanagerUrl string
	Status          string
	GeneratorUrl    string
}

func init() {
	GitlabApiToken = os.Getenv("GITLAB_API_TOKEN")
	ProjectId = os.Getenv("GITLAB_PROJECT_ID")
}

func main() {
	r := gin.Default()
	gitlab := newGitlabClient(GitlabApiToken)
	r.POST("/alert", func(c *gin.Context) {
		body := Payload{}
		if err := c.BindJSON(&body); err != nil {
			c.AbortWithError(http.StatusBadRequest, err)
			return
		}
		err := createGitlabIssue(gitlab, body)
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{})
	})
	r.Run()

}

func newGitlabClient(apiToken string) *gitlab.Client {
	git, err := gitlab.NewClient(apiToken)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	return git
}

func createGitlabIssue(git *gitlab.Client, body Payload) error {

	for _, alert := range body.Alerts {

		fingerprint := alert.Fingerprint
		issueResolved := alert.Status != "firing"

		title := alert.Annotations["summary"]
		if len(title) == 0 {
			title = alert.Annotations["message"]
		}
		descriptionData := DescriptionData{
			Fingerprint:     alert.Fingerprint,
			Annotations:     alert.Annotations,
			StartsAt:        alert.StartsAt.String(),
			EndsAt:          alert.EndsAt.String(),
			AlertmanagerUrl: body.ExternalURL,
			GeneratorUrl:    alert.GeneratorURL,
			Status:          alert.Status,
		}

		if len(title) >= 255 {
			title = title[:255]
		}
		var b bytes.Buffer
		t := template.Must(template.ParseFiles("description-template.txt"))
		t.Execute(&b, descriptionData)
		description := b.String()

		if exists, issueIId, err := checkIfFingerprintExists(git, fingerprint); err == nil && exists {
			err = addNoteToIssue(git, alert, issueIId, issueResolved, description)
			if err != nil {
				fmt.Println(fmt.Errorf(err.Error()))
			}
			continue
		}

		labels := make([]string, len(alert.Labels)+1)
		labels = append(labels, "fingerprint::"+alert.Fingerprint)
		for key, val := range alert.Labels {
			labels = append(labels, key+"::"+val)
		}
		issueOptions := &gitlab.CreateIssueOptions{
			Title:       &title,
			Description: &description,
			Labels:      (*gitlab.Labels)(&labels),
		}
		_, response, err := git.Issues.CreateIssue(ProjectId, issueOptions)
		if err != nil {
			fmt.Println(fmt.Errorf(err.Error()))
			return err
		}
		if response.StatusCode != 201 {
			fmt.Println(fmt.Errorf(strconv.Itoa(response.StatusCode)))
			return err
		}
	}

	return nil
}

func checkIfFingerprintExists(git *gitlab.Client, fingerprint string) (bool, int, error) {
	labels := &gitlab.Labels{"fingerprint::" + fingerprint}
	state := "opened"
	listOptions := &gitlab.ListIssuesOptions{
		State:  &state,
		Labels: labels,
	}
	issues, _, err := git.Issues.ListIssues(listOptions)
	if err != nil {
		fmt.Println(fmt.Errorf(err.Error()))
		return false, 0, err
	}
	if len(issues) == 0 {
		return false, 0, nil
	}

	return true, issues[0].IID, nil
}

func addNoteToIssue(git *gitlab.Client, alert Alert, issueIId int, issueResolved bool, description string) error {
	var body string
	if issueResolved {
		body = "_**Alert resolved**_\n\n" + description + "\n/close"
	} else {
		body = "_**Alert triggerd again**_\n\n" + description
	}
	noteOptions := &gitlab.CreateIssueNoteOptions{
		Body: &body,
	}
	_, _, err := git.Notes.CreateIssueNote(ProjectId, issueIId, noteOptions)

	return err
}
