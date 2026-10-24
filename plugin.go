// SPDX-FileCopyrightText: The OpenArtifact Authors
//
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Supported values of the "action" setting.
const (
	ActionUpload        = "upload"
	ActionBundle        = "bundle"
	ActionPromote       = "promote"
	ActionUploadPromote = "upload+promote"
)

// MinOAVersion is the oldest oa CLI that ships the upload/bundle/promote commands.
const MinOAVersion = "0.10.0"

// Settings is the plugin configuration after env resolution.
type Settings struct {
	Server string
	Token  string // never logged, never put on a command line
	Action string

	// upload
	Repo       string
	Files      []string
	TargetPath string
	Flat       bool
	Properties map[string]string

	// build metadata (defaults come from the CI environment)
	BuildName   string
	BuildNumber string
	BuildURL    string
	VCSRevision string

	// bundle
	BundleName        string
	BundleVersion     string
	BundleDescription string
	BundlePipeline    string
	BundlePaths       []string // paths inside Repo added explicitly, besides the uploaded build
	GPGKey            string

	// promote
	Stage      string
	TargetRepo string
	Notes      string

	ResultFile string
	OAPath     string
}

// Step is one oa invocation. Args never contain the token.
type Step struct {
	Name string // upload | bundle create | bundle add | bundle seal | promote
	Args []string
}

func (s Settings) hasUpload() bool {
	return s.Action == ActionUpload || s.Action == ActionUploadPromote
}

func (s Settings) hasBundle() bool {
	return s.Action == ActionBundle || s.Action == ActionUploadPromote
}

func (s Settings) hasPromote() bool {
	return s.Action == ActionPromote || s.Action == ActionUploadPromote
}

// Validate reports every missing or inconsistent setting at once.
func (s Settings) Validate() error {
	var errs []error
	need := func(ok bool, msg string) {
		if !ok {
			errs = append(errs, errors.New(msg))
		}
	}
	switch s.Action {
	case ActionUpload, ActionBundle, ActionPromote, ActionUploadPromote:
	default:
		return fmt.Errorf(
			"invalid action %q: want upload, bundle, promote or upload+promote",
			s.Action,
		)
	}
	need(s.Server != "", "server is required")
	need(s.Token != "", "token is required (use from_secret)")
	if s.hasUpload() {
		need(s.Repo != "", "repo is required for upload")
		need(len(s.Files) > 0, "files is required for upload")
	}
	if s.hasBundle() || s.hasPromote() {
		need(
			s.BundleName != "",
			"bundle_name is required (no repository name found in the CI environment)",
		)
		need(
			s.BundleVersion != "",
			"bundle_version is required (not running on a tag, so CI_COMMIT_TAG/DRONE_TAG is empty)",
		)
	}
	if s.hasBundle() {
		fromBuild := s.BuildName != "" && s.BuildNumber != ""
		need(fromBuild || len(s.BundlePaths) > 0,
			"bundle needs build_name and build_number (CI env) or bundle_paths to know what to add")
		need(len(s.BundlePaths) == 0 || s.Repo != "", "repo is required with bundle_paths")
	}
	if s.hasPromote() {
		need(s.Stage != "", "stage is required for promote")
	}
	for k := range s.Properties {
		need(k != "" && !strings.ContainsAny(k, "=\n"), fmt.Sprintf("invalid property key %q", k))
	}
	return errors.Join(errs...)
}

func (s Settings) global() []string {
	return []string{"--server", s.Server, "--format", "json"}
}

// Plan maps the settings to the ordered oa invocations.
func (s Settings) Plan() ([]Step, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	var steps []Step
	add := func(name string, args ...string) {
		steps = append(steps, Step{Name: name, Args: append(s.global(), args...)})
	}

	if s.hasUpload() {
		args := append([]string{"upload", s.Repo}, s.Files...)
		args = appendOpt(args, "--target-path", s.TargetPath)
		if s.Flat {
			args = append(args, "--flat")
		}
		for _, k := range slices.Sorted(maps.Keys(s.Properties)) {
			args = append(args, "--property", k+"="+s.Properties[k])
		}
		args = appendOpt(args, "--build-name", s.BuildName)
		args = appendOpt(args, "--build-number", s.BuildNumber)
		args = appendOpt(args, "--build-url", s.BuildURL)
		args = appendOpt(args, "--vcs-revision", s.VCSRevision)
		add("upload", args...)
	}

	if s.hasBundle() {
		create := []string{"bundle", "create", s.BundleName, s.BundleVersion}
		create = appendOpt(create, "--description", s.BundleDescription)
		create = appendOpt(create, "--pipeline", s.BundlePipeline)
		add("bundle create", create...)

		bundleAdd := []string{"bundle", "add", s.BundleName, s.BundleVersion}
		if s.BuildName != "" && s.BuildNumber != "" {
			bundleAdd = append(bundleAdd, "--from-build", s.BuildName+"/"+s.BuildNumber)
		}
		if len(s.BundlePaths) > 0 {
			bundleAdd = append(bundleAdd, "--repo", s.Repo)
			for _, p := range s.BundlePaths {
				bundleAdd = append(bundleAdd, "--path", p)
			}
		}
		add("bundle add", bundleAdd...)

		seal := []string{"bundle", "seal", s.BundleName, s.BundleVersion}
		seal = appendOpt(seal, "--gpg-key", s.GPGKey)
		add("bundle seal", seal...)
	}

	if s.hasPromote() {
		promote := []string{"promote", s.BundleName, s.BundleVersion, "--stage", s.Stage}
		promote = appendOpt(promote, "--target-repo", s.TargetRepo)
		promote = appendOpt(promote, "--notes", s.Notes)
		add("promote", promote...)
	}
	return steps, nil
}

func appendOpt(args []string, flag, value string) []string {
	if value == "" {
		return args
	}
	return append(args, flag, value)
}
