package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//go:embed renderer/certificates.py
var certificateRenderScript string

// One document conversion per app process bounds memory and CPU use while the
// HTTP server continues handling normal requests.
var certificateRenderSlots = make(chan struct{}, 1)

func defaultCertificateRenderer(ctx context.Context, request CertificateRenderRequest) (CertificateRenderResult, error) {
	var result CertificateRenderResult
	// macOS /var and /tmp are symlink aliases; Python resolves output paths to
	// /private/... while Go's MkdirTemp preserves the original alias.
	canonicalDir, err := filepath.EvalSymlinks(request.OutputDir)
	if err != nil {
		return result, err
	}
	request.OutputDir = canonicalDir
	select {
	case certificateRenderSlots <- struct{}{}:
		defer func() { <-certificateRenderSlots }()
	case <-ctx.Done():
		return result, fail(503, "证书生成等待超时，请稍后重试")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	payload, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	python := env("CERTIFICATE_PYTHON", "python3")
	cmd := exec.CommandContext(ctx, python, "-c", certificateRenderScript)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Dir = request.OutputDir
	// The worker receives no SMTP credentials, database URL or session secrets.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "HOME=" + request.OutputDir, "TMPDIR=" + request.OutputDir}
	if font := os.Getenv("CERTIFICATE_FONT"); font != "" {
		cmd.Env = append(cmd.Env, "CERTIFICATE_FONT="+font)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 3 * time.Second
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &bytes.Buffer{}
	err = cmd.Run()
	if ctx.Err() != nil {
		return result, fail(503, "证书转换超时，请精简模板或重试")
	}
	var failure struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(output.Bytes(), &failure)
	if failure.Error != "" {
		return result, fail(400, failure.Error)
	}
	if err != nil {
		return result, fail(503, "证书生成服务暂不可用，请联系管理员")
	}
	if err = json.Unmarshal(output.Bytes(), &result); err != nil {
		return result, fail(503, "证书生成服务返回异常")
	}
	// Outputs must stay in the per-request private directory.
	paths := []string{result.PDFPath, result.PreviewPath}
	for _, page := range result.Pages {
		paths = append(paths, page.PreviewPath)
	}
	for _, path := range paths {
		rel, e := filepath.Rel(request.OutputDir, path)
		if e != nil || filepath.IsAbs(rel) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return result, fail(503, "证书生成路径异常")
		}
	}
	return result, nil
}
