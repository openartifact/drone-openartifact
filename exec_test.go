// SPDX-FileCopyrightText: The OpenArtifact Authors
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	version string
	calls   [][]string
	outputs map[string]string // first subcommand word(s) -> stdout
	failOn  string
}

func (f *fakeRunner) Run(_ context.Context, args []string) ([]byte, error) {
	f.calls = append(f.calls, args)
	if len(args) == 3 && args[2] == "version" {
		return []byte(`{"version":"` + f.version + `"}`), nil
	}
	key := strings.Join(args[4:min(len(args), 6)], " ")
	if f.failOn != "" && strings.HasPrefix(key, f.failOn) {
		return nil, errors.New("exit status 1")
	}
	for prefix, out := range f.outputs {
		if strings.HasPrefix(key, prefix) {
			return []byte(out), nil
		}
	}
	return []byte(`{}`), nil
}

const uploadJSON = `{"uploaded":[{"repo":"releases","path":"app/a.tar.gz","sha256":"0123456789abcdef0123","size":10,"url":"https://oa/x"}]}`

func newFake() *fakeRunner {
	return &fakeRunner{version: "0.10.0", outputs: map[string]string{
		"upload":  uploadJSON,
		"bundle":  `{"bundle":"app","version":"1.2.3","state":"sealed","url":"https://oa/b"}`,
		"promote": `{"bundle":"app","version":"1.2.3","state":"promoted","url":"https://oa/b","promoted":{"stage":"prod","target_repo":"prod","items":3}}`,
	}}
}

func TestExecUploadPromote(t *testing.T) {
	s := base()
	s.Action = ActionUploadPromote
	s.ResultFile = filepath.Join(t.TempDir(), "res.json")
	var out bytes.Buffer
	p := &Plugin{Settings: s, Runner: newFake(), Out: &out}
	require.NoError(t, p.Exec(t.Context()))

	assert.Contains(
		t,
		out.String(),
		"uploaded releases/app/a.tar.gz  10 bytes  sha256:0123456789ab",
	)
	assert.Contains(t, out.String(), `promoted to stage "prod" (repo prod), 3 items`)
	assert.NotContains(t, out.String(), "secret")

	data, err := os.ReadFile(s.ResultFile)
	require.NoError(t, err)
	var res Result
	require.NoError(t, json.Unmarshal(data, &res))
	assert.True(t, res.Success)
	assert.Len(t, res.Steps, 5)
	assert.Len(t, res.Uploaded, 1)
	assert.Equal(t, "promoted", res.Bundle.State)
}

func TestExecOldOA(t *testing.T) {
	f := newFake()
	f.version = "0.9.0"
	p := &Plugin{Settings: base(), Runner: f, Out: &bytes.Buffer{}}
	err := p.Exec(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too old")
	assert.Len(t, f.calls, 1, "no command may run on an old oa")
}

func TestExecPreReleaseVersionAccepted(t *testing.T) {
	f := newFake()
	f.version = "v0.10.0-rc.1"
	p := &Plugin{Settings: base(), Runner: f, Out: &bytes.Buffer{}}
	require.NoError(t, p.Exec(t.Context()))
}

func TestExecFailureWritesResultAndStops(t *testing.T) {
	s := base()
	s.Action = ActionUploadPromote
	s.ResultFile = filepath.Join(t.TempDir(), "res.json")
	f := newFake()
	f.failOn = "bundle seal"
	p := &Plugin{Settings: s, Runner: f, Out: &bytes.Buffer{}}
	err := p.Exec(t.Context())
	require.ErrorContains(t, err, "oa bundle seal failed")

	data, rerr := os.ReadFile(s.ResultFile)
	require.NoError(t, rerr)
	var res Result
	require.NoError(t, json.Unmarshal(data, &res))
	assert.False(t, res.Success)
	assert.NotEmpty(t, res.Error)
	assert.Equal(t, "bundle seal", res.Steps[len(res.Steps)-1].Name)
	for _, c := range f.calls {
		assert.NotContains(t, c, "promote", "promote must not run after a failed seal")
	}
}

func TestExecEmptyUploadFails(t *testing.T) {
	f := newFake()
	f.outputs["upload"] = `{"uploaded":[]}`
	p := &Plugin{Settings: base(), Runner: f, Out: &bytes.Buffer{}}
	require.ErrorContains(t, p.Exec(t.Context()), "no uploaded files")
}

func TestExecValidationErrorWritesResult(t *testing.T) {
	s := base()
	s.Token = ""
	s.ResultFile = filepath.Join(t.TempDir(), "res.json")
	p := &Plugin{Settings: s, Runner: newFake(), Out: &bytes.Buffer{}}
	require.Error(t, p.Exec(t.Context()))
	_, err := os.Stat(s.ResultFile)
	require.NoError(t, err)
}

func TestRedactor(t *testing.T) {
	var b bytes.Buffer
	n, err := redactor{w: &b, secret: "s3cr3t"}.Write([]byte("token s3cr3t leaked\n"))
	require.NoError(t, err)
	assert.Equal(t, 20, n)
	assert.Equal(t, "token *** leaked\n", b.String())
}

func TestExecRunnerPassesTokenViaEnvOnly(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "oa")
	require.NoError(
		t,
		os.WriteFile(
			script,
			[]byte("#!/bin/sh\necho \"args:$*\" >&2\necho \"{\\\"t\\\":\\\"$OA_TOKEN\\\"}\"\n"),
			0o755,
		),
	)
	var stderr bytes.Buffer
	out, err := ExecRunner{
		Path:   script,
		Token:  "s3cr3t",
		Stderr: &stderr,
	}.Run(
		t.Context(),
		[]string{"a", "b"},
	)
	require.NoError(t, err)
	assert.JSONEq(t, `{"t":"***"}`, string(out), "token echoed by oa must be masked")
	assert.Equal(t, "args:a b\n", stderr.String())
}
