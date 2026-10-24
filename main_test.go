// SPDX-FileCopyrightText: The OpenArtifact Authors
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func resolve(t *testing.T, env map[string]string) Settings {
	t.Helper()
	for _, k := range []string{
		"PLUGIN_BUILD_NAME", "CI_REPO", "DRONE_REPO", "PLUGIN_BUILD_NUMBER", "CI_PIPELINE_NUMBER",
		"DRONE_BUILD_NUMBER", "CI_PIPELINE_URL", "DRONE_BUILD_LINK", "CI_COMMIT_SHA", "DRONE_COMMIT_SHA",
		"CI_REPO_NAME", "DRONE_REPO_NAME", "CI_COMMIT_TAG", "DRONE_TAG", "OA_SERVER", "OA_TOKEN",
	} {
		t.Setenv(k, "x")
		require.NoError(t, os.Unsetenv(k))
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	var got Settings
	app := newApp()
	app.Action = func(_ context.Context, c *cli.Command) error {
		var err error
		got, err = settingsFrom(c)
		return err
	}
	require.NoError(t, app.Run(t.Context(), []string{"drone-openartifact"}))
	return got
}

func TestSettingsWoodpeckerEnv(t *testing.T) {
	s := resolve(t, map[string]string{
		"PLUGIN_SERVER":       "https://oa.example.com",
		"PLUGIN_TOKEN":        "secret",
		"PLUGIN_REPO":         "releases",
		"PLUGIN_FILES":        "dist/*.tar.gz, dist/*.zip",
		"PLUGIN_FLAT":         "true",
		"PLUGIN_PROPERTIES":   `{"team":"core","retention":30}`,
		"CI_REPO":             "acme/app",
		"CI_REPO_NAME":        "app",
		"CI_PIPELINE_NUMBER":  "42",
		"CI_PIPELINE_URL":     "https://ci.example.com/repos/1/pipeline/42",
		"CI_COMMIT_SHA":       "abc123",
		"CI_COMMIT_TAG":       "v1.2.3",
		"PLUGIN_ACTION":       "Upload+Promote",
		"PLUGIN_STAGE":        "prod",
		"PLUGIN_BUNDLE_PATHS": "a/b.jar",
	})
	assert.Equal(t, "https://oa.example.com", s.Server)
	assert.Equal(t, []string{"dist/*.tar.gz", "dist/*.zip"}, s.Files)
	assert.True(t, s.Flat)
	assert.Equal(t, map[string]string{"team": "core", "retention": "30"}, s.Properties)
	assert.Equal(t, "acme/app", s.BuildName)
	assert.Equal(t, "42", s.BuildNumber)
	assert.Equal(t, "https://ci.example.com/repos/1/pipeline/42", s.BuildURL)
	assert.Equal(t, "abc123", s.VCSRevision)
	assert.Equal(t, "app", s.BundleName)
	assert.Equal(t, "v1.2.3", s.BundleVersion)
	assert.Equal(t, ActionUploadPromote, s.Action)
	assert.Equal(t, []string{"a/b.jar"}, s.BundlePaths)
}

func TestSettingsDroneEnv(t *testing.T) {
	s := resolve(t, map[string]string{
		"DRONE_REPO":         "acme/app",
		"DRONE_REPO_NAME":    "app",
		"DRONE_BUILD_NUMBER": "7",
		"DRONE_BUILD_LINK":   "https://drone.example.com/acme/app/7",
		"DRONE_COMMIT_SHA":   "def456",
		"DRONE_TAG":          "1.0.0",
		"OA_SERVER":          "https://oa.example.com",
		"OA_TOKEN":           "t",
	})
	assert.Equal(t, "acme/app", s.BuildName)
	assert.Equal(t, "7", s.BuildNumber)
	assert.Equal(t, "https://drone.example.com/acme/app/7", s.BuildURL)
	assert.Equal(t, "def456", s.VCSRevision)
	assert.Equal(t, "1.0.0", s.BundleVersion)
	assert.Equal(t, "https://oa.example.com", s.Server)
	assert.Equal(t, "t", s.Token)
	assert.Equal(t, ActionUpload, s.Action)
}

func TestPluginSettingOverridesCI(t *testing.T) {
	s := resolve(t, map[string]string{
		"PLUGIN_BUILD_NUMBER": "99", "CI_PIPELINE_NUMBER": "42",
		"PLUGIN_BUNDLE_VERSION": "2.0.0", "CI_COMMIT_TAG": "v1",
	})
	assert.Equal(t, "99", s.BuildNumber)
	assert.Equal(t, "2.0.0", s.BundleVersion)
}

func TestParseProperties(t *testing.T) {
	got, err := parseProperties("a=1, b=x=y")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"a": "1", "b": "x=y"}, got)

	got, err = parseProperties("")
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = parseProperties("novalue")
	require.Error(t, err)
	_, err = parseProperties("{bad")
	require.Error(t, err)
}
