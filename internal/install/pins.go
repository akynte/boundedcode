package install

import "runtime"

// Pinned versions. Keep in sync with docs/architecture/upstream-components.md
// and the license matrix. Digests are the sha256 values GitHub reports for
// each release asset (and, for Linux, the values scripts/install-deps.sh
// pinned before).
const (
	GitleaksVersion = "8.30.1"
	CBMVersion      = "0.11.0"
	// LlamaTag is the llama.cpp release; LlamaBuild is the build tag on the
	// same commit (7fe450e19305b828c199d602c23a8337aaa1f03b) that carries
	// the prebuilt archives.
	LlamaTag   = "v0.5.0"
	LlamaBuild = "b11146"
)

func gh(repo, tag, name string) string {
	return "https://github.com/" + repo + "/releases/download/" + tag + "/" + name
}

func gitleaks(name, sha string) Asset {
	return Asset{gh("gitleaks/gitleaks", "v"+GitleaksVersion, name), sha}
}

func cbm(name, sha string) Asset {
	return Asset{gh("DeusData/codebase-memory-mcp", "v"+CBMVersion, name), sha}
}

func llama(name, sha string) Asset {
	return Asset{gh("ggml-org/llama.cpp", LlamaBuild, name), sha}
}

// Platform is GOOS/GOARCH.
func Platform() string { return runtime.GOOS + "/" + runtime.GOARCH }

// Gitleaks archives per platform.
var Gitleaks = map[string]Asset{
	"linux/amd64":   gitleaks("gitleaks_8.30.1_linux_x64.tar.gz", "551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb"),
	"linux/arm64":   gitleaks("gitleaks_8.30.1_linux_arm64.tar.gz", "e4a487ee7ccd7d3a7f7ec08657610aa3606637dab924210b3aee62570fb4b080"),
	"darwin/amd64":  gitleaks("gitleaks_8.30.1_darwin_x64.tar.gz", "dfe101a4db2255fc85120ac7f3d25e4342c3c20cf749f2c20a18081af1952709"),
	"darwin/arm64":  gitleaks("gitleaks_8.30.1_darwin_arm64.tar.gz", "b40ab0ae55c505963e365f271a8d3846efbc170aa17f2607f13df610a9aeb6a5"),
	"windows/amd64": gitleaks("gitleaks_8.30.1_windows_x64.zip", "d29144deff3a68aa93ced33dddf84b7fdc26070add4aa0f4513094c8332afc4e"),
	"windows/arm64": gitleaks("gitleaks_8.30.1_windows_arm64.zip", "b95f5e4f5c425cedca7ee203d9afd29597e692c4924a12ed42f970537c72cc0f"),
}

// CBM (codebase-memory-mcp) archives per platform.
var CBM = map[string]Asset{
	"linux/amd64":   cbm("codebase-memory-mcp-linux-amd64.tar.gz", "032b33c1833919a2d1de67ff6367fa6ea46aee8689c86ef223c88fae3b6e4536"),
	"linux/arm64":   cbm("codebase-memory-mcp-linux-arm64.tar.gz", "c0e46c87cf37e35f1ac0bd9cc7e1d8b0ca4ef40034e1008805d709fa52a4e38a"),
	"darwin/amd64":  cbm("codebase-memory-mcp-darwin-amd64.tar.gz", "dbf1c73bfcbde64e7dde4cd1320da7afc02e2c972ee1789ae039521411f5132e"),
	"darwin/arm64":  cbm("codebase-memory-mcp-darwin-arm64.tar.gz", "4dee7f38b63740e6751d7a7ed7eb10291c1f2a3ea2415f599dc68370ca0a2d18"),
	"windows/amd64": cbm("codebase-memory-mcp-windows-amd64.zip", "6eb6beaf261b19e419766e78baf93cbc3cf1c6338cff8fb7c0234859f96d1685"),
	"windows/arm64": cbm("codebase-memory-mcp-windows-arm64.zip", "52b29881214fce47d529e098308b1de77c40f6812e20a34d588ee4c25b84fd1a"),
}

// LlamaVariant is one prebuilt llama.cpp build: the archive and, for CUDA
// builds, the CUDA runtime archive unpacked next to it.
type LlamaVariant struct {
	Name    string // cpu | metal | cuda
	Archive Asset
	Runtime *Asset
}

// Llama lists the prebuilt llama.cpp builds per platform, best first for a
// machine with an NVIDIA GPU (the CPU build is the fallback).
var Llama = map[string][]LlamaVariant{
	"darwin/arm64": {{Name: "metal", Archive: llama("llama-b11146-bin-macos-arm64.tar.gz", "1ad3f9eff80edb9dbef4259ad564d1720612ef7eea48fa4afed0e54f5f3d5711")}},
	"darwin/amd64": {{Name: "cpu", Archive: llama("llama-b11146-bin-macos-x64.tar.gz", "305f0e3a17d2c01eb205cd0a62128357f1ec3b55329cb084d94e5ec0115d7a3b")}},
	"windows/amd64": {
		{Name: "cuda", Archive: llama("llama-b11146-bin-win-cuda-12.4-x64.zip", "3c806a6ceccc3dae1c743ceb1a1fb2cce5b76f40bfbd4c6b7b8afb6ef45a5807"),
			Runtime: &Asset{gh("ggml-org/llama.cpp", LlamaBuild, "cudart-llama-bin-win-cuda-12.4-x64.zip"), "8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6"}},
		{Name: "cpu", Archive: llama("llama-b11146-bin-win-cpu-x64.zip", "14cf1303ca9ac3abd94816850532f9f9a69ac66fbaca3776fc6f9061c2fac1d1")},
	},
	"windows/arm64": {{Name: "cpu", Archive: llama("llama-b11146-bin-win-cpu-arm64.zip", "1727d241f3bf6d27360e984e851cf013928fd655bf89f8628e70da027f377b7d")}},
	"linux/amd64": {
		{Name: "cuda", Archive: llama("llama-b11146-bin-ubuntu-cuda-12.8-x64.tar.gz", "c2ab9e19838513ff69d1af8d999ad717dd3c7ee4714ac04c7ed5ab9077c50e4e"),
			Runtime: &Asset{gh("ggml-org/llama.cpp", LlamaBuild, "cudart-llama-b11146-bin-ubuntu-cuda-12.8-x64.tar.gz"), "1466daea60aad1144819e151b2bae19d54556cf1da6c129c4f55a5ded2637c25"}},
		{Name: "cpu", Archive: llama("llama-b11146-bin-ubuntu-x64.tar.gz", "c150306eb16b5ab696f76a8bdf810c35fd98a24e82158742e6fa28f420ff8410")},
	},
	"linux/arm64": {{Name: "cpu", Archive: llama("llama-b11146-bin-ubuntu-arm64.tar.gz", "4aeda6fe68831547e49b7fa87607383ca5352b3d72ca5f70d52ed265f58c131f")}},
}

// PickLlama chooses the build for this machine: CUDA when an NVIDIA GPU is
// present and the platform has a CUDA build, else the first non-CUDA one.
func PickLlama(platform string, nvidia bool) (LlamaVariant, bool) {
	vs := Llama[platform]
	for _, v := range vs {
		if v.Name == "cuda" && nvidia {
			return v, true
		}
	}
	for _, v := range vs {
		if v.Name != "cuda" {
			return v, true
		}
	}
	return LlamaVariant{}, false
}

// CodexVersion is the Codex CLI release the frontier route is tested with.
const CodexVersion = "0.156.1"

// CodexLinux are Linux builds of the Codex CLI per container architecture.
// On macOS and Windows the contained frontier route runs codex in a Linux
// container, where the host's own codex (a Mach-O or Windows program, or a
// Node script) cannot run; this build is mounted instead.
var CodexLinux = map[string]Asset{
	"amd64": {gh("openai/codex", "rust-v"+CodexVersion, "codex-x86_64-unknown-linux-musl.tar.gz"), "aff46539a83aff86e3c62c592bce2c50d95391f9df289afaf03a50c01d14533d"},
	"arm64": {gh("openai/codex", "rust-v"+CodexVersion, "codex-aarch64-unknown-linux-musl.tar.gz"), "558e12aaa6dacb335ec47240bf9721db8a54746806d64f01185a403f44f79b72"},
}

// CodexLinuxMember is the binary inside a CodexLinux archive.
func CodexLinuxMember(arch string) string {
	if arch == "arm64" {
		return "codex-aarch64-unknown-linux-musl"
	}
	return "codex-x86_64-unknown-linux-musl"
}
