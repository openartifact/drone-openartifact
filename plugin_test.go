// SPDX-FileCopyrightText: The OpenArtifact Authors
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func base() Settings {
	return Settings{
		Server: "https://oa.example.com", Token: "secret", Action: ActionUpload,
		Repo: "releases", Files: []string{"dist/*.tar.gz"},
		BuildName: "acme/app", BuildNumber: "42", BuildURL: "https://ci/42", VCSRevision: "abc",
		BundleName: "app", BundleVersion: "1.2.3", Stage: "prod",
	}
}

func TestPlanUpload(t *testing.T) {
	s := base()
	s.TargetPath = "app/1.2.3"
	s.Flat = true
	s.Properties = map[string]string{"z": "last", "a": "first"}
	steps, err := s.Plan()
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, []string{
		"--server", "https://oa.example.com", "--format", "json",
		"upload", "releases", "dist/*.tar.gz",
		"--target-path", "app/1.2.3", "--flat",
		"--property", "a=first", "--property", "z=last",
		"--build-name", "acme/app", "--build-number", "42",
		"--build-url", "https://ci/42", "--vcs-revision", "abc",
	}, steps[0].Args)
}

func TestPlanNeverContainsToken(t *testing.T) {
	s := base()
	s.Action = ActionUploadPromote
	steps, err := s.Plan()
	require.NoError(t, err)
	for _, st := range steps {
		assert.NotContains(t, st.Args, "secret")
		assert.NotContains(t, st.Args, "--token")
	}
}

func TestPlanUploadPromote(t *testing.T) {
	s := base()
	s.Action = ActionUploadPromote
	s.BundleDescription = "release 1.2.3"
	s.GPGKey = "ABCD"
	s.TargetRepo = "prod-releases"
	s.Notes = "shipped via CI"
	steps, err := s.Plan()
	require.NoError(t, err)
	var names []string
	for _, st := range steps {
		names = append(names, st.Name)
	}
	assert.Equal(
		t,
		[]string{"upload", "bundle create", "bundle add", "bundle seal", "promote"},
		names,
	)
	g := []string{"--server", "https://oa.example.com", "--format", "json"}
	assert.Equal(
		t,
		append(g, "bundle", "create", "app", "1.2.3", "--description", "release 1.2.3"),
		steps[1].Args,
	)
	assert.Equal(
		t,
		append(g, "bundle", "add", "app", "1.2.3", "--from-build", "acme/app/42"),
		steps[2].Args,
	)
	assert.Equal(t, append(g, "bundle", "seal", "app", "1.2.3", "--gpg-key", "ABCD"), steps[3].Args)
	assert.Equal(t, append(g, "promote", "app", "1.2.3", "--stage", "prod",
		"--target-repo", "prod-releases", "--notes", "shipped via CI"), steps[4].Args)
}

func TestPlanBundleExplicitPaths(t *testing.T) {
	s := base()
	s.Action = ActionBundle
	s.BuildName, s.BuildNumber = "", ""
	s.BundlePaths = []string{"a/b.jar", "a/c.jar"}
	steps, err := s.Plan()
	require.NoError(t, err)
	require.Len(t, steps, 3)
	assert.Equal(t, []string{
		"--server",
		"https://oa.example.com",
		"--format",
		"json",
		"bundle",
		"add",
		"app",
		"1.2.3",
		"--repo",
		"releases",
		"--path",
		"a/b.jar",
		"--path",
		"a/c.jar",
	}, steps[1].Args)
}

func TestPlanPromoteOnly(t *testing.T) {
	s := base()
	s.Action = ActionPromote
	s.Repo, s.Files = "", nil
	steps, err := s.Plan()
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, "promote", steps[0].Name)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Settings)
		want string
	}{
		{"bad action", func(s *Settings) { s.Action = "nope" }, "invalid action"},
		{"no server", func(s *Settings) { s.Server = "" }, "server is required"},
		{"no token", func(s *Settings) { s.Token = "" }, "token is required"},
		{"no repo", func(s *Settings) { s.Repo = "" }, "repo is required"},
		{"no files", func(s *Settings) { s.Files = nil }, "files is required"},
		{
			"no version",
			func(s *Settings) { s.Action = ActionPromote; s.BundleVersion = "" },
			"bundle_version is required",
		},
		{
			"no stage",
			func(s *Settings) { s.Action = ActionPromote; s.Stage = "" },
			"stage is required",
		},
		{
			"bad prop",
			func(s *Settings) { s.Properties = map[string]string{"a=b": "c"} },
			"invalid property key",
		},
		{
			"bundle nothing",
			func(s *Settings) { s.Action = ActionBundle; s.BuildNumber = "" },
			"bundle needs",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			tc.mut(&s)
			_, err := s.Plan()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
