module github.com/clictl/cli

go 1.25.0

require (
	github.com/dop251/goja v0.0.0-20260311135729-065cd970411c
	github.com/landlock-lsm/go-landlock v0.7.0
	github.com/spf13/cobra v1.8.1
	go.etcd.io/bbolt v1.4.3
	golang.org/x/sys v0.42.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	kernel.org/pub/linux/libs/security/libcap/psx v1.2.77 // indirect
)

// Supply chain safety: use our forks for security-sensitive dependencies.
// Forks are kept in sync with upstream manually after code review.
//
// JS engine (executes user code): https://github.com/clictl/goja
replace github.com/dop251/goja => github.com/clictl/goja v0.0.0-20260311135729-065cd970411c
