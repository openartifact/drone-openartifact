// SPDX-FileCopyrightText: The OpenArtifact Authors
//
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command fakeoa stands in for the oa CLI in image smoke tests: it prints the
// documented JSON shapes and appends every invocation to $FAKE_OA_LOG. It is
// static so it runs in the distroless image.
package main

import (
	"fmt"
	"os"
	"strings"
)

func say(s string) { _, _ = os.Stdout.WriteString(s + "\n") }

func sayf(format string, a ...any) { _, _ = fmt.Fprintf(os.Stdout, format, a...) }

func main() {
	args := os.Args[1:]
	if log := os.Getenv("FAKE_OA_LOG"); log != "" {
		if f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			// The token must only arrive through the environment.
			fmt.Fprintf(
				f,
				"token_env=%t args=%s\n",
				os.Getenv("OA_TOKEN") != "",
				strings.Join(args, " "),
			)
			_ = f.Close()
		}
	}
	// skip global flags: --server X --format json
	for len(args) >= 2 && strings.HasPrefix(args[0], "--") {
		args = args[2:]
	}
	if len(args) == 0 {
		os.Exit(2)
	}
	switch {
	case args[0] == "version":
		say(`{"version":"0.10.0"}`)
	case args[0] == "upload":
		say(`{"uploaded":[{"repo":"` + args[1] + `","path":"fake/file.txt","sha256":"` +
			strings.Repeat("ab", 32) + `","size":5,"url":"https://oa.invalid/fake/file.txt"}]}`)
	case args[0] == "bundle" && len(args) > 2:
		state := map[string]string{"create": "open", "add": "open", "seal": "sealed"}[args[1]]
		sayf(
			`{"bundle":%q,"version":%q,"state":%q,"url":"https://oa.invalid/b"}`+"\n",
			args[2],
			args[3],
			state,
		)
	case args[0] == "promote":
		sayf(`{"bundle":%q,"version":%q,"state":"promoted","url":"https://oa.invalid/b",`+
			`"promoted":{"stage":"prod","target_repo":"","items":1}}`+"\n", args[1], args[2])
	default:
		fmt.Fprintln(os.Stderr, "fakeoa: unsupported:", args)
		os.Exit(2)
	}
}
