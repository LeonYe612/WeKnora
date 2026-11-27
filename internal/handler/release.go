package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 部署环境标识，由 compose 注入。
const (
	envDeployEnv         = "WEKNORA_DEPLOY_ENV"
	envReleaseAgentURL   = "RELEASE_AGENT_URL"
	envReleaseAgentToken = "RELEASE_AGENT_TOKEN"
	// 候选版本来自哪个仓库的工作流
	releaseRepo     = "LeonYe612/WeKnora"
	releaseWorkflow = "build-fork-image.yml"
)

func deployEnv() string {
	if v := strings.TrimSpace(os.Getenv(envDeployEnv)); v != "" {
		return v
	}
	return "unknown"
}

type releaseAgentConf struct {
	base  string
	token string
}

func releaseAgentConfig() (releaseAgentConf, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv(envReleaseAgentURL)), "/")
	token := strings.TrimSpace(os.Getenv(envReleaseAgentToken))
	if base == "" {
		return releaseAgentConf{}, fmt.Errorf("未配置 %s，发布功能不可用", envReleaseAgentURL)
	}
	if token == "" {
		return releaseAgentConf{}, fmt.Errorf("未配置 %s，发布功能不可用", envReleaseAgentToken)
	}
	return releaseAgentConf{base: base, token: token}, nil
}

// callAgent 转发到宿主 release-agent，原样回传状态码与 body。
func callAgent(c *gin.Context, method, path string, body any, timeout time.Duration) {
	conf, err := releaseAgentConfig()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": err.Error()})
		return
	}

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
			return
		}
		reader = bytes.NewReader(b)
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, conf.base+path, reader)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	req.Header.Set("X-Release-Token", conf.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   "无法连接发布服务（宿主 release-agent 未启动？）: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	c.Data(resp.StatusCode, "application/json; charset=utf-8", raw)
}

// GetReleaseStatus 返回当前部署版本、容器状态与最近一次发布结果。
func (h *SystemHandler) GetReleaseStatus(c *gin.Context) {
	callAgent(c, http.MethodGet, "/status", nil, 45*time.Second)
}

// TriggerRelease 触发发布。只接受 sha7，digest 由 agent 从 pull 输出解析。
func (h *SystemHandler) TriggerRelease(c *gin.Context) {
	var req struct {
		SHA7 string `json:"sha7"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求体非法: " + err.Error()})
		return
	}
	req.SHA7 = strings.TrimSpace(strings.ToLower(req.SHA7))
	if req.SHA7 == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "sha7 不能为空"})
		return
	}
	callAgent(c, http.MethodPost, "/deploy", gin.H{"sha7": req.SHA7}, 30*time.Second)
}

// RollbackRelease 回滚到上一个成功版本。
func (h *SystemHandler) RollbackRelease(c *gin.Context) {
	callAgent(c, http.MethodPost, "/rollback", gin.H{}, 30*time.Second)
}

// GetReleaseCandidates 列出 CI 最近成功构建，供前端选择。
// 只读 GitHub 公共 API，不需要 token（匿名 60 次/小时，足够人工操作）。
func (h *SystemHandler) GetReleaseCandidates(c *gin.Context) {
	type run struct {
		HeadSHA      string `json:"head_sha"`
		CreatedAt    string `json:"created_at"`
		DisplayTitle string `json:"display_title"`
		HTMLURL      string `json:"html_url"`
		Conclusion   string `json:"conclusion"`
	}
	type runsResp struct {
		WorkflowRuns []run `json:"workflow_runs"`
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	url := fmt.Sprintf(
		"https://api.github.com/repos/%s/actions/workflows/%s/runs?per_page=15&status=success",
		releaseRepo, releaseWorkflow)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "查询 GitHub 失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false,
			"error":   fmt.Sprintf("GitHub 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(raw))),
		})
		return
	}

	var parsed runsResp
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "解析 GitHub 响应失败: " + err.Error()})
		return
	}

	type candidate struct {
		SHA7  string `json:"sha7"`
		SHA   string `json:"sha"`
		Title string `json:"title"`
		At    string `json:"at"`
		URL   string `json:"url"`
	}
	out := make([]candidate, 0, len(parsed.WorkflowRuns))
	for _, r := range parsed.WorkflowRuns {
		if len(r.HeadSHA) < 7 {
			continue
		}
		out = append(out, candidate{
			SHA7:  r.HeadSHA[:7],
			SHA:   r.HeadSHA,
			Title: r.DisplayTitle,
			At:    r.CreatedAt,
			URL:   r.HTMLURL,
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"candidates": out}})
}
