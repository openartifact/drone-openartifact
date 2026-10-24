// SPDX-FileCopyrightText: The OpenArtifact Authors
//
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command drone-openartifact is a Drone / Woodpecker plugin that uploads
// artifacts to OpenArtifact and promotes releases through the oa CLI.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/urfave/cli/v3"
)

// Version is set at build time.
var Version = "dev"

// env declares the settings-derived sources of a flag: PLUGIN_* (the
// settings: block) and the oa CLI's own OA_* variables.
func env(names ...string) cli.ValueSourceChain { return cli.EnvVars(names...) }

// ciEnv returns the first non-empty variable. Woodpecker exports CI_* and Drone
// DRONE_*; unlike a flag source chain this skips variables that are set but
// empty (CI_COMMIT_TAG is empty outside tag events).
func ciEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// orCI uses the explicit setting, else the pipeline environment.
func orCI(explicit string, names ...string) string {
	if explicit != "" {
		return explicit
	}
	return ciEnv(names...)
}

func main() { os.Exit(realMain()) }

func realMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := newApp().Run(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

func newApp() *cli.Command {
	return &cli.Command{
		Name:    "drone-openartifact",
		Usage:   "upload artifacts to OpenArtifact and promote releases",
		Version: Version,
		Action:  run,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server",
				Usage:   "OpenArtifact server URL",
				Sources: env("PLUGIN_SERVER", "OA_SERVER"),
			},
			&cli.StringFlag{
				Name:    "token",
				Usage:   "API token (use from_secret)",
				Sources: env("PLUGIN_TOKEN", "OA_TOKEN"),
			},
			&cli.StringFlag{
				Name:    "action",
				Usage:   "upload | bundle | promote | upload+promote",
				Value:   ActionUpload,
				Sources: env("PLUGIN_ACTION"),
			},
			&cli.StringFlag{Name: "repo", Usage: "target repository", Sources: env("PLUGIN_REPO")},
			&cli.StringSliceFlag{
				Name:    "files",
				Usage:   "files or globs to upload",
				Sources: env("PLUGIN_FILES"),
			},
			&cli.StringFlag{
				Name:    "target-path",
				Usage:   "path prefix inside the repository",
				Sources: env("PLUGIN_TARGET_PATH"),
			},
			&cli.BoolFlag{
				Name:    "flat",
				Usage:   "drop local directories from uploaded paths",
				Sources: env("PLUGIN_FLAT"),
			},
			&cli.StringFlag{
				Name:    "properties",
				Usage:   "artifact properties, JSON object or k=v,k=v",
				Sources: env("PLUGIN_PROPERTIES"),
			},
			&cli.StringFlag{
				Name:    "build-name",
				Usage:   "build name property",
				Sources: env("PLUGIN_BUILD_NAME"),
			},
			&cli.StringFlag{
				Name:    "build-number",
				Usage:   "build number property",
				Sources: env("PLUGIN_BUILD_NUMBER"),
			},
			&cli.StringFlag{
				Name:    "build-url",
				Usage:   "build URL property",
				Sources: env("PLUGIN_BUILD_URL"),
			},
			&cli.StringFlag{
				Name:    "vcs-revision",
				Usage:   "commit SHA property",
				Sources: env("PLUGIN_VCS_REVISION"),
			},
			&cli.StringFlag{
				Name:    "bundle-name",
				Usage:   "release bundle name",
				Sources: env("PLUGIN_BUNDLE_NAME"),
			},
			&cli.StringFlag{
				Name:    "bundle-version",
				Usage:   "release bundle version",
				Sources: env("PLUGIN_BUNDLE_VERSION"),
			},
			&cli.StringFlag{
				Name:    "bundle-description",
				Usage:   "release bundle description",
				Sources: env("PLUGIN_BUNDLE_DESCRIPTION"),
			},
			&cli.StringFlag{
				Name:    "bundle-pipeline",
				Usage:   "promotion pipeline name",
				Sources: env("PLUGIN_BUNDLE_PIPELINE"),
			},
			&cli.StringSliceFlag{
				Name:    "bundle-paths",
				Usage:   "extra paths in repo to add to the bundle",
				Sources: env("PLUGIN_BUNDLE_PATHS"),
			},
			&cli.StringFlag{
				Name:    "gpg-key",
				Usage:   "GPG key id used to seal the bundle",
				Sources: env("PLUGIN_GPG_KEY"),
			},
			&cli.StringFlag{Name: "stage", Usage: "promotion stage", Sources: env("PLUGIN_STAGE")},
			&cli.StringFlag{
				Name:    "target-repo",
				Usage:   "repository the promotion copies into",
				Sources: env("PLUGIN_TARGET_REPO"),
			},
			&cli.StringFlag{Name: "notes", Usage: "promotion notes", Sources: env("PLUGIN_NOTES")},
			&cli.StringFlag{
				Name:    "result-file",
				Usage:   "write the JSON result here",
				Sources: env("PLUGIN_RESULT_FILE"),
			},
			&cli.StringFlag{
				Name:    "oa-path",
				Usage:   "oa binary",
				Value:   "oa",
				Sources: env("PLUGIN_OA_PATH"),
			},
		},
	}
}

func run(ctx context.Context, c *cli.Command) error {
	s, err := settingsFrom(c)
	if err != nil {
		return err
	}
	p := &Plugin{
		Settings: s,
		Runner:   ExecRunner{Path: s.OAPath, Token: s.Token, Stderr: os.Stderr},
		Out:      os.Stdout,
	}
	return p.Exec(ctx)
}

func settingsFrom(c *cli.Command) (Settings, error) {
	props, err := parseProperties(c.String("properties"))
	if err != nil {
		return Settings{}, err
	}
	return Settings{
		Server:     c.String("server"),
		Token:      c.String("token"),
		Action:     strings.ToLower(strings.TrimSpace(c.String("action"))),
		Repo:       c.String("repo"),
		Files:      cleanList(c.StringSlice("files")),
		TargetPath: c.String("target-path"),
		Flat:       c.Bool("flat"),
		Properties: props,
		BuildName:  orCI(c.String("build-name"), "CI_REPO", "DRONE_REPO"),
		BuildNumber: orCI(
			c.String("build-number"),
			"CI_PIPELINE_NUMBER",
			"DRONE_BUILD_NUMBER",
		),
		BuildURL:          orCI(c.String("build-url"), "CI_PIPELINE_URL", "DRONE_BUILD_LINK"),
		VCSRevision:       orCI(c.String("vcs-revision"), "CI_COMMIT_SHA", "DRONE_COMMIT_SHA"),
		BundleName:        orCI(c.String("bundle-name"), "CI_REPO_NAME", "DRONE_REPO_NAME"),
		BundleVersion:     orCI(c.String("bundle-version"), "CI_COMMIT_TAG", "DRONE_TAG"),
		BundleDescription: c.String("bundle-description"),
		BundlePipeline:    c.String("bundle-pipeline"),
		BundlePaths:       cleanList(c.StringSlice("bundle-paths")),
		GPGKey:            c.String("gpg-key"),
		Stage:             c.String("stage"),
		TargetRepo:        c.String("target-repo"),
		Notes:             c.String("notes"),
		ResultFile:        c.String("result-file"),
		OAPath:            c.String("oa-path"),
	}, nil
}

func cleanList(in []string) []string {
	var out []string
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// parseProperties accepts what Drone and Woodpecker produce for a YAML map
// setting (a JSON object) and, for convenience, "k=v,k2=v2".
func parseProperties(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	props := map[string]string{}
	if raw == "" {
		return props, nil
	}
	if strings.HasPrefix(raw, "{") {
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, fmt.Errorf("properties: invalid JSON object: %w", err)
		}
		for k, v := range m {
			switch t := v.(type) {
			case string:
				props[k] = t
			case nil:
				props[k] = ""
			default:
				b, _ := json.Marshal(t)
				props[k] = string(b)
			}
		}
		return props, nil
	}
	for pair := range strings.SplitSeq(raw, ",") {
		if pair = strings.TrimSpace(pair); pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("properties: %q is not key=value", pair)
		}
		props[strings.TrimSpace(k)] = v
	}
	return props, nil
}
