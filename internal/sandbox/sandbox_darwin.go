// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
//go:build darwin

package sandbox

import (
	"bytes"
	"context"
	"fmt"
	osexec "os/exec"
	"text/template"
)

// sbProfileRestrictedTmpl is used for MCP servers with a known binary path.
// It restricts both reads and writes to declared paths.
var sbProfileRestrictedTmpl = template.Must(template.New("sb-restricted").Parse(`(version 1)
(deny default)

;; Process control
(allow process-exec)
(allow process-fork)
(allow sysctl-read)
(allow mach-lookup)
(allow mach-register)
(allow signal (target self))
(allow iokit-open)

;; System read paths (shared libraries, runtime)
(allow file-read*
    (subpath "/usr/lib")
    (subpath "/usr/share")
    (subpath "/usr/local")
    (subpath "/opt/homebrew")
    (subpath "/System")
    (subpath "/Library/Frameworks")
    (subpath "/dev")
    (subpath "/private/etc/ssl")
    (subpath "/private/etc/resolv.conf")
    (subpath "/private/etc/hosts")
    (literal "/etc/ssl")
    (literal "/etc/resolv.conf")
    (literal "/etc/hosts")
{{- range .ReadPaths }}
    (subpath "{{ . }}")
{{- end }}
)

;; Writable paths (tmp, working dir, spec-declared)
(allow file-read* file-write*
    (subpath "/tmp")
    (subpath "/private/tmp")
    (subpath "/private/var/tmp")
{{- range .WritePaths }}
    (subpath "{{ . }}")
{{- end }}
)

;; Network
(allow network*)

;; IPC for stdio transport
(allow file-read* file-write*
    (literal "/dev/stdin")
    (literal "/dev/stdout")
    (literal "/dev/stderr")
    (literal "/dev/null")
    (literal "/dev/tty")
    (literal "/dev/urandom")
    (literal "/dev/random")
)
`))

// sbProfilePackageTmpl is used for package-manager-based MCP servers (npx, uvx).
// Allows unrestricted reads since package managers resolve modules across many
// paths, but restricts writes to cache dirs, tmp, and working directory.
// Sensitive directories (.ssh, .aws, browser profiles) are protected by env
// scrubbing - the process never receives credentials for those services.
var sbProfilePackageTmpl = template.Must(template.New("sb-package").Parse(`(version 1)
(deny default)

;; Process control
(allow process-exec)
(allow process-fork)
(allow sysctl-read)
(allow mach-lookup)
(allow mach-register)
(allow signal (target self))
(allow iokit-open)

;; Reads: allow all (package managers need broad filesystem access for module resolution)
(allow file-read*)

;; Writable paths (tmp, working dir, package cache)
(allow file-read* file-write*
    (subpath "/tmp")
    (subpath "/private/tmp")
    (subpath "/private/var/tmp")
{{- range .WritePaths }}
    (subpath "{{ . }}")
{{- end }}
)

;; Network
(allow network*)

;; IPC for stdio transport
(allow file-read* file-write*
    (literal "/dev/stdin")
    (literal "/dev/stdout")
    (literal "/dev/stderr")
    (literal "/dev/null")
    (literal "/dev/tty")
    (literal "/dev/urandom")
    (literal "/dev/random")
)
`))

type sbProfileData struct {
	ReadPaths  []string
	WritePaths []string
}

func applyAndStart(ctx context.Context, cmd *osexec.Cmd, policy *Policy) error {
	data := sbProfileData{
		ReadPaths:  AllowedReadPaths(policy),
		WritePaths: AllowedWritePaths(policy),
	}

	// Use the package-manager template for npx/uvx-based MCP servers
	tmpl := sbProfileRestrictedTmpl
	if policy.Spec.Package != nil {
		tmpl = sbProfilePackageTmpl
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("rendering sandbox profile: %w", err)
	}
	profile := buf.String()

	// Wrap the original command with sandbox-exec.
	// Use cmd.Path (resolved absolute path) instead of cmd.Args[0] (basename)
	// so sandbox-exec can find the binary without relying on PATH lookup.
	originalPath := cmd.Path
	originalArgs := cmd.Args[1:] // skip argv[0] (basename), use resolved path instead
	cmd.Path = "/usr/bin/sandbox-exec"
	cmd.Args = append([]string{"sandbox-exec", "-p", profile, "--", originalPath}, originalArgs...)

	if err := cmd.Start(); err != nil {
		// sandbox-exec failed (maybe SIP disabled or binary missing) - report error
		// so the caller can fall back
		return fmt.Errorf("sandbox-exec: %w", err)
	}

	return nil
}
