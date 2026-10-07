# Install BoundedCode for the current Windows user:
#
#   irm https://raw.githubusercontent.com/akynte/boundedcode/main/scripts/install.ps1 | iex
#
# Installs boundedcode.exe and its short name bcode.exe into
# %LOCALAPPDATA%\Programs\BoundedCode\bin (no administrator rights needed),
# verified against the release's SHA256SUMS, and adds that folder to the
# user's PATH. Then run `bcode` in a project: the first run sets up the rest
# (inference server, model or cloud provider, tools, sandbox), asking first.
#
# Requirements: Windows 10/11 x64, Git for Windows, Docker Desktop.
#
# env: BC_VERSION  release tag to install (default: the newest release with a
#                  Windows binary)
#      BC_BIN_DIR  install folder
#      BC_REPO     GitHub repository (default: akynte/boundedcode)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$Repo = if ($env:BC_REPO) { $env:BC_REPO } else { 'akynte/boundedcode' }
$BinDir = if ($env:BC_BIN_DIR) { $env:BC_BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\BoundedCode\bin' }

function Say($msg) { Write-Host "◆ $msg" -ForegroundColor Magenta }
function Die($msg) { Write-Host "✘ $msg" -ForegroundColor Red; exit 1 }

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { Die "unsupported CPU $($env:PROCESSOR_ARCHITECTURE) (amd64 and arm64 are supported)." }
}
$Asset = "boundedcode-windows-$arch.exe"
if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Die 'Git for Windows is required (BoundedCode works on git repositories): https://git-scm.com/download/win'
}

[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("bcode-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    # TryRelease downloads and verifies the binary of a release; $false when
    # the release has none.
    function TryRelease($tag) {
        $base = "https://github.com/$Repo/releases/download/$tag"
        $exe = Join-Path $tmp $Asset
        try { Invoke-WebRequest -UseBasicParsing -Uri "$base/$Asset" -OutFile $exe } catch { return $false }
        $sums = Join-Path $tmp 'SHA256SUMS'
        try { Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile $sums } catch { Die "release $tag has no SHA256SUMS; refusing an unverified binary" }
        $line = Get-Content $sums | Where-Object { $_ -match "\s$([regex]::Escape($Asset))$" } | Select-Object -First 1
        if (-not $line) { Die "release $tag lists no checksum for $Asset" }
        $want = ($line -split '\s+')[0].ToLower()
        $got = (Get-FileHash -Algorithm SHA256 -Path $exe).Hash.ToLower()
        if ($got -ne $want) { Die "checksum mismatch for $Asset ($tag)" }
        Say "downloaded release $tag (checksum verified)"
        return $true
    }

    $ok = $false
    if ($env:BC_VERSION) {
        $ok = TryRelease $env:BC_VERSION
        if (-not $ok) { Die "release $($env:BC_VERSION) has no $Asset" }
    } else {
        $releases = Invoke-RestMethod -UseBasicParsing -Uri "https://api.github.com/repos/$Repo/releases?per_page=10"
        foreach ($r in $releases) {
            if (TryRelease $r.tag_name) { $ok = $true; break }
        }
        if (-not $ok) { Die "no release ships a Windows binary yet; build from source with Go: git clone https://github.com/$Repo && cd boundedcode && go build -o bcode.exe ./cmd/boundedcode" }
    }

    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    Copy-Item -Force (Join-Path $tmp $Asset) (Join-Path $BinDir 'boundedcode.exe')
    Copy-Item -Force (Join-Path $tmp $Asset) (Join-Path $BinDir 'bcode.exe')
    $version = & (Join-Path $BinDir 'boundedcode.exe') version
    Say "installed $version to $BinDir (boundedcode.exe, bcode.exe)"

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not (($userPath -split ';') -contains $BinDir)) {
        [Environment]::SetEnvironmentVariable('Path', ($(if ($userPath) { "$userPath;" } else { '' }) + $BinDir), 'User')
        Say "added $BinDir to your user PATH (open a new terminal to use it)"
    }
    Write-Host ''
    Write-Host '  Next: cd into a git repository and run'
    Write-Host ''
    Write-Host '    bcode'
    Write-Host ''
    Write-Host '  The first run checks what is missing and sets it up with your permission.'
    Write-Host "  Uninstall: delete $BinDir and remove it from your user PATH; data is under"
    Write-Host '  %USERPROFILE%\.config, .local\share, .local\state and .cache in boundedcode folders,'
    Write-Host '  and the sandbox image (docker rmi boundedcode-openhands:local).'
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
