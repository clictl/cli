// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
//go:build darwin

package sandbox

import (
	"bytes"
	"fmt"
	"os"
	osexec "os/exec"
	"text/template"
	"time"
)

// C3.13: macOS backend - sandbox-exec with generated SBPL profiles.

// sbplSandboxTemplate generates a sandbox-exec profile that restricts filesystem and
// network access according to the sandbox configuration.
var sbplSandboxTemplate = template.Must(template.New("sbpl-sandbox").Parse(`(version 1)
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

;; Writable paths
(allow file-read* file-write*
    (subpath "/tmp")
    (subpath "/private/tmp")
    (subpath "/private/var/tmp")
{{- range .WritePaths }}
    (subpath "{{ . }}")
{{- end }}
)

;; Network access
{{- if eq .NetworkMode "none" }}
(deny network*)
{{- else if eq .NetworkMode "host" }}
(allow network*)
{{- else }}
(allow network*
    (remote ip "localhost:*")
    (remote ip "127.0.0.1:*")
    (remote ip "::1:*")
{{- range .AllowedHosts }}
    (remote ip "{{ . }}:*")
{{- end }}
)
{{- end }}

;; Credential forwarding
{{- if .SSHAgent }}
;; SSH agent socket forwarded
(allow file-read* file-write*
    (regex #"^/private/tmp/com\.apple\.launchd\..*/Listeners$"))
{{- end }}

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

type sandboxSBPLData struct {
	ReadPaths    []string
	WritePaths   []string
	NetworkMode  string
	AllowedHosts []string
	SSHAgent     bool
}

// runMacOSSandbox executes a command inside a macOS sandbox-exec sandbox.
func runMacOSSandbox(cfg *Config, command string, args []string) (*Result, error) {
	start := time.Now()
	result := &Result{}

	// Build SBPL profile
	data := sandboxSBPLData{
		ReadPaths:    macSandboxReadPaths(cfg),
		WritePaths:   macSandboxWritePaths(cfg),
		NetworkMode:  string(cfg.NetworkMode),
		AllowedHosts: cfg.AllowedHosts,
		SSHAgent:     cfg.Credentials.SSHAgent && !cfg.Credentials.IsCredentialDenied("ssh"),
	}

	var profileBuf bytes.Buffer
	if err := sbplSandboxTemplate.Execute(&profileBuf, data); err != nil {
		return nil, fmt.Errorf("generating sandbox profile: %w", err)
	}

	// Build the sandbox-exec command
	sandboxArgs := []string{"-p", profileBuf.String(), "--", command}
	sandboxArgs = append(sandboxArgs, args...)
	cmd := osexec.Command("sandbox-exec", sandboxArgs...)

	if len(cfg.Env) > 0 {
		cmd.Env = cfg.Env
	}
	if cfg.WorkingDir != "" {
		cmd.Dir = cfg.WorkingDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting sandbox-exec: %w", err)
	}

	// Wait with timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	if cfg.Timeout > 0 {
		select {
		case err := <-done:
			result.ExitCode = exitCodeFromError(err)
		case <-time.After(cfg.Timeout):
			cmd.Process.Kill()
			<-done
			result.TimedOut = true
			result.ExitCode = -1
		}
	} else {
		err := <-done
		result.ExitCode = exitCodeFromError(err)
	}

	result.Stdout = stdout.Bytes()
	result.Stderr = stderr.Bytes()
	result.Duration = time.Since(start)

	return result, nil
}

// macSandboxReadPaths returns paths that should be readable in the sandbox.
func macSandboxReadPaths(cfg *Config) []string {
	paths := SystemReadOnlyPaths()

	for _, m := range cfg.Mounts {
		if m.ReadOnly {
			paths = append(paths, m.Source)
		}
	}

	// C3.11: Credential forwarding - git config read-only
	if cfg.Credentials.GitConfig && !cfg.Credentials.IsCredentialDenied("git") {
		home, _ := os.UserHomeDir()
		if home != "" {
			gitConfig := home + "/.gitconfig"
			if _, err := os.Stat(gitConfig); err == nil {
				paths = append(paths, gitConfig)
			}
		}
	}

	// C3.11: Credential forwarding - npm config read-only
	if cfg.Credentials.NPMConfig && !cfg.Credentials.IsCredentialDenied("npm") {
		home, _ := os.UserHomeDir()
		if home != "" {
			npmrc := home + "/.npmrc"
			if _, err := os.Stat(npmrc); err == nil {
				paths = append(paths, npmrc)
			}
		}
	}

	return paths
}

// macSandboxWritePaths returns paths that should be writable in the sandbox.
func macSandboxWritePaths(cfg *Config) []string {
	var paths []string

	if cfg.WorkingDir != "" {
		paths = append(paths, cfg.WorkingDir)
	}

	for _, m := range cfg.Mounts {
		if !m.ReadOnly {
			paths = append(paths, m.Source)
		}
	}

	return paths
}

// runLinuxNS is not available on Darwin.
func runLinuxNS(cfg *Config, command string, args []string) (*Result, error) {
	return nil, fmt.Errorf("Linux namespace sandbox is not available on macOS")
}
