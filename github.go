package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type GitHubClient struct {
	baseURL string
	token   string
	owner   string
	repo    string
}

type Issue struct {
	Number    int           `json:"number"`
	Title     string        `json:"title"`
	Body      string        `json:"body"`
	State     string        `json:"state"`
	HTMLURL   string        `json:"html_url"`
	User      GitHubUser    `json:"user"`
	Assignees []GitHubUser  `json:"assignees"`
	Labels    []GitHubLabel `json:"labels"`
}

type GitHubUser struct {
	Login string `json:"login"`
}

type GitHubLabel struct {
	Name string `json:"name"`
}

type IssueComment struct {
	ID        int64      `json:"id"`
	Body      string     `json:"body"`
	HTMLURL   string     `json:"html_url"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	User      GitHubUser `json:"user"`
}

func (gh *GitHubClient) httpClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func (gh *GitHubClient) GetIssue(ctx context.Context, issueNumber int) (Issue, error) {
	var issue Issue
	err := gh.doJSON(ctx, http.MethodGet, gh.issueURL(issueNumber), nil, &issue)
	return issue, err
}

func (gh *GitHubClient) ListIssueComments(ctx context.Context, issueNumber, limit int) ([]IssueComment, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []IssueComment
	err := gh.doJSON(
		ctx,
		http.MethodGet,
		gh.issueCommentsURL(issueNumber)+"?per_page="+strconv.Itoa(limit),
		nil,
		&out,
	)
	return out, err
}

func (gh *GitHubClient) PostIssueComment(ctx context.Context, issueNumber int, body string) (IssueComment, error) {
	var out IssueComment
	err := gh.doJSON(ctx, http.MethodPost, gh.issueCommentsURL(issueNumber), map[string]string{
		"body": body,
	}, &out)
	return out, err
}

func (gh *GitHubClient) AssignIssue(ctx context.Context, issueNumber int, assignees []string) (Issue, error) {
	var out Issue
	err := gh.doJSON(ctx, http.MethodPatch, gh.issueURL(issueNumber), map[string][]string{
		"assignees": assignees,
	}, &out)
	return out, err
}

func (gh *GitHubClient) SetIssueLabels(ctx context.Context, issueNumber int, labels []string) ([]GitHubLabel, error) {
	var out []GitHubLabel
	err := gh.doJSON(ctx, http.MethodPut, gh.issueLabelsURL(issueNumber), labels, &out)
	return out, err
}

func (gh *GitHubClient) AddIssueLabels(ctx context.Context, issueNumber int, labels []string) ([]GitHubLabel, error) {
	var out []GitHubLabel
	err := gh.doJSON(ctx, http.MethodPost, gh.issueLabelsURL(issueNumber), map[string][]string{
		"labels": labels,
	}, &out)
	return out, err
}

func (gh *GitHubClient) RemoveIssueLabel(ctx context.Context, issueNumber int, label string) error {
	return gh.doJSON(ctx, http.MethodDelete, gh.issueLabelsURL(issueNumber)+"/"+url.PathEscape(label), nil, nil)
}

func (gh *GitHubClient) doJSON(ctx context.Context, method, u string, payload interface{}, out interface{}) error {
	var body io.Reader
	if payload != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(payload); err != nil {
			return err
		}
		body = buf
	}

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+gh.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := gh.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github API %s %s failed: status=%d body=%s", method, u, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (gh *GitHubClient) issueURL(issueNumber int) string {
	return fmt.Sprintf("%s/repos/%s/%s/issues/%d", gh.baseURL, gh.owner, gh.repo, issueNumber)
}

func (gh *GitHubClient) issueCommentsURL(issueNumber int) string {
	return fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", gh.baseURL, gh.owner, gh.repo, issueNumber)
}

func (gh *GitHubClient) issueLabelsURL(issueNumber int) string {
	return fmt.Sprintf("%s/repos/%s/%s/issues/%d/labels", gh.baseURL, gh.owner, gh.repo, issueNumber)
}
