// SPDX-FileCopyrightText: The OpenArtifact Authors
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Runner executes one oa invocation and returns its stdout. Stderr goes to
// the step log. The token is handed over through the environment only.
type Runner interface {
	Run(ctx context.Context, args []string) ([]byte, error)
}

// ExecRunner runs a real oa binary.
type ExecRunner struct {
	Path   string
	Token  string
	Stderr io.Writer
}

// Run implements Runner.
func (r ExecRunner) Run(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.Path, args...)
	cmd.Env = append(os.Environ(), "OA_TOKEN="+r.Token)
	var out bytes.Buffer
	cmd.Stdout = redactor{w: &out, secret: r.Token}
	cmd.Stderr = redactor{w: r.Stderr, secret: r.Token}
	err := cmd.Run()
	return out.Bytes(), err
}

// redactor masks the secret in anything written through it. oa never echoes
// the token, but a server error body or a proxy might.
type redactor struct {
	w      io.Writer
	secret string
}

func (r redactor) Write(p []byte) (int, error) {
	if r.secret == "" {
		return r.w.Write(p)
	}
	if _, err := r.w.Write(bytes.ReplaceAll(p, []byte(r.secret), []byte("***"))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// UploadedItem is one entry of `oa upload` JSON output.
type UploadedItem struct {
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	URL    string `json:"url"`
}

// BundleOut is the JSON shape of the bundle/promote commands.
type BundleOut struct {
	Bundle   string `json:"bundle"`
	Version  string `json:"version"`
	State    string `json:"state"`
	URL      string `json:"url"`
	Promoted *struct {
		Stage      string          `json:"stage"`
		TargetRepo string          `json:"target_repo"`
		Items      json.RawMessage `json:"items"`
	} `json:"promoted"`
}

// StepResult records one executed step in the result file.
type StepResult struct {
	Name   string          `json:"name"`
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Output json.RawMessage `json:"output,omitempty"`
}

// Result is the machine-readable outcome written to result_file.
type Result struct {
	Action   string         `json:"action"`
	Success  bool           `json:"success"`
	Error    string         `json:"error,omitempty"`
	Uploaded []UploadedItem `json:"uploaded,omitempty"`
	Bundle   *BundleOut     `json:"bundle,omitempty"`
	Steps    []StepResult   `json:"steps"`
}

// Plugin drives the oa CLI for one pipeline step.
type Plugin struct {
	Settings Settings
	Runner   Runner
	Out      io.Writer // step log (summary)
}

// Exec runs the plan, prints a summary and writes the result file. The
// result file is written on failure too.
func (p *Plugin) Exec(ctx context.Context) error {
	res := &Result{Action: p.Settings.Action, Steps: []StepResult{}}
	err := p.run(ctx, res)
	res.Success = err == nil
	if err != nil {
		res.Error = err.Error()
	}
	if werr := p.writeResult(res); werr != nil {
		err = errors.Join(err, werr)
	}
	return err
}

func (p *Plugin) run(ctx context.Context, res *Result) error {
	steps, err := p.Settings.Plan()
	if err != nil {
		return err
	}
	if err := p.checkVersion(ctx); err != nil {
		return err
	}
	for _, st := range steps {
		fmt.Fprintf(p.Out, "==> oa %s\n", strings.Join(quoteAll(st.Args), " "))
		out, err := p.Runner.Run(ctx, st.Args)
		sr := StepResult{Name: st.Name, OK: err == nil}
		if json.Valid(bytes.TrimSpace(out)) {
			sr.Output = json.RawMessage(bytes.TrimSpace(out))
		}
		if err != nil {
			sr.Error = err.Error()
			res.Steps = append(res.Steps, sr)
			return fmt.Errorf("oa %s failed: %w", st.Name, err)
		}
		res.Steps = append(res.Steps, sr)
		if err := p.collect(st, sr.Output, res); err != nil {
			return err
		}
	}
	p.summary(res)
	return nil
}

func (p *Plugin) collect(st Step, out json.RawMessage, res *Result) error {
	switch st.Name {
	case "upload":
		var u struct {
			Uploaded []UploadedItem `json:"uploaded"`
		}
		if err := json.Unmarshal(out, &u); err != nil {
			return fmt.Errorf("oa upload: unparsable JSON output: %w", err)
		}
		if len(u.Uploaded) == 0 {
			return errors.New("oa upload reported no uploaded files (check the files glob)")
		}
		res.Uploaded = append(res.Uploaded, u.Uploaded...)
	case "bundle create", "bundle add", "bundle seal", "promote":
		var b BundleOut
		if err := json.Unmarshal(out, &b); err != nil {
			return fmt.Errorf("oa %s: unparsable JSON output: %w", st.Name, err)
		}
		res.Bundle = &b
	}
	return nil
}

func (p *Plugin) summary(res *Result) {
	w := p.Out
	fmt.Fprintln(w)
	fmt.Fprintf(w, "OpenArtifact: %s succeeded\n", p.Settings.Action)
	for _, u := range res.Uploaded {
		fmt.Fprintf(
			w,
			"  uploaded %s/%s  %d bytes  sha256:%s\n",
			u.Repo,
			u.Path,
			u.Size,
			shortSHA(u.SHA256),
		)
		if u.URL != "" {
			fmt.Fprintf(w, "           %s\n", u.URL)
		}
	}
	if b := res.Bundle; b != nil {
		fmt.Fprintf(w, "  bundle   %s %s [%s]\n", b.Bundle, b.Version, b.State)
		if b.Promoted != nil {
			fmt.Fprintf(w, "  promoted to stage %q", b.Promoted.Stage)
			if b.Promoted.TargetRepo != "" {
				fmt.Fprintf(w, " (repo %s)", b.Promoted.TargetRepo)
			}
			if n := countItems(b.Promoted.Items); n >= 0 {
				fmt.Fprintf(w, ", %d items", n)
			}
			fmt.Fprintln(w)
		}
		if b.URL != "" {
			fmt.Fprintf(w, "  %s\n", b.URL)
		}
	}
}

func (p *Plugin) writeResult(res *Result) error {
	if p.Settings.ResultFile == "" {
		return nil
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(p.Settings.ResultFile, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write result file: %w", err)
	}
	return nil
}

var semverRe = regexp.MustCompile(`v?(\d+)\.(\d+)\.(\d+)`)

// checkVersion fails clearly when oa is older than MinOAVersion. Pre-release
// suffixes are ignored so 0.10.0-rc1 and dev builds of 0.10.0 pass.
func (p *Plugin) checkVersion(ctx context.Context) error {
	out, err := p.Runner.Run(ctx, []string{"--format", "json", "version"})
	if err != nil {
		return fmt.Errorf("oa version failed (oa >= %s required): %w", MinOAVersion, err)
	}
	var got string
	var v struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(out, &v) == nil {
		got = v.Version
	} else {
		got = string(out)
	}
	have, ok := parseVersion(got)
	want, _ := parseVersion(MinOAVersion)
	if !ok {
		return fmt.Errorf(
			"cannot determine oa version from %q (oa >= %s required)",
			strings.TrimSpace(got),
			MinOAVersion,
		)
	}
	if compareVersion(have, want) < 0 {
		return fmt.Errorf(
			"oa %s is too old: the plugin needs oa >= %s (upload, bundle and promote commands)",
			got,
			MinOAVersion,
		)
	}
	return nil
}

func parseVersion(s string) ([3]int, bool) {
	m := semverRe.FindStringSubmatch(s)
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

func compareVersion(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			return a[i] - b[i]
		}
	}
	return 0
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func countItems(raw json.RawMessage) int {
	if len(raw) == 0 {
		return -1
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		return len(arr)
	}
	return -1
}

// quoteAll renders args for the log, quoting those with whitespace.
func quoteAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n\"'") {
			out[i] = strconv.Quote(a)
		} else {
			out[i] = a
		}
	}
	return out
}
