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
#      BC_DOWNLOAD_BASE  where release assets are fetched from, as
#                  BASE/TAG/ASSET (default: the GitHub releases). SHA256SUMS
#                  comes from the same place, so use only a mirror you trust.
# The script block keeps the strict settings below out of the user's session.
& {
    $ErrorActionPreference = 'Stop'
    Set-StrictMode -Version Latest

    $Repo = if ($env:BC_REPO) { $env:BC_REPO } else { 'akynte/boundedcode' }
    $DownloadBase = if ($env:BC_DOWNLOAD_BASE) { $env:BC_DOWNLOAD_BASE.TrimEnd('/') } else { "https://github.com/$Repo/releases/download" }
    $BinDir = if ($env:BC_BIN_DIR) { $env:BC_BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\BoundedCode\bin' }

    function Say($msg) { Write-Host "◆ $msg" -ForegroundColor Magenta }
    # Die stops the installer with a message. It throws rather than exits: under
    # `irm | iex` an exit would close the user's PowerShell window.
    function Die($msg) { throw $msg }

    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("bcode-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    $failed = $false
    try {
        $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
            'AMD64' { 'amd64' }
            'ARM64' { 'arm64' }
            default { Die "unsupported CPU $($env:PROCESSOR_ARCHITECTURE) (amd64 and arm64 are supported)." }
        }
        $Asset = "boundedcode-windows-$arch.exe"
        if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
            Die 'Git for Windows is required (BoundedCode works on git repositories): https://git-scm.com/download/win'
        }

        # TryRelease downloads and verifies the binary of a release; $false when
        # the release has none.
        function TryRelease($tag) {
            $base = "$DownloadBase/$tag"
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
            $tags = @()
            try {
                $tags = @(Invoke-RestMethod -UseBasicParsing -Uri "https://api.github.com/repos/$Repo/releases?per_page=10" | ForEach-Object { $_.tag_name })
            } catch {
                # The API allows 60 unauthenticated requests an hour; the
                # releases feed is not rate-limited the same way.
                $feed = (Invoke-WebRequest -UseBasicParsing -Uri "https://github.com/$Repo/releases.atom").Content
                $tags = @([regex]::Matches($feed, '/releases/tag/([^"<]+)"') | ForEach-Object { $_.Groups[1].Value })
            }
            foreach ($t in $tags) {
                if (TryRelease $t) { $ok = $true; break }
            }
            if (-not $ok) { Die "no release ships a Windows binary yet; build from source with Go: git clone https://github.com/$Repo && cd boundedcode && go build -o bcode.exe ./cmd/boundedcode" }
        }

        New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
        # Copy next to the target, then rename: an interrupted install never
        # leaves a half-written program.
        foreach ($name in 'boundedcode.exe', 'bcode.exe') {
            $staged = Join-Path $BinDir ".$name.new"
            Copy-Item -Force (Join-Path $tmp $Asset) $staged
            Move-Item -Force $staged (Join-Path $BinDir $name)
        }
        $version = & (Join-Path $BinDir 'boundedcode.exe') version
        Say "installed $version to $BinDir (boundedcode.exe, bcode.exe)"

        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (-not (($userPath -split ';') -contains $BinDir)) {
            [Environment]::SetEnvironmentVariable('Path', ($(if ($userPath) { "$userPath;" } else { '' }) + $BinDir), 'User')
            Say "added $BinDir to your user PATH (open a new terminal to use it)"
        }
        $other = Get-Command bcode -All -ErrorAction SilentlyContinue | Where-Object { $_.Source -and ((Split-Path $_.Source) -ne $BinDir) } | Select-Object -First 1
        if ($other) {
            Write-Host "  Warning: another bcode is on your PATH ($($other.Source)); BoundedCode's is $BinDir\bcode.exe." -ForegroundColor Yellow
        }
        Write-Host ''
        Write-Host '  Next: cd into a git repository and run'
        Write-Host ''
        Write-Host '    bcode'
        Write-Host ''
        Write-Host '  The first run shows what is missing and sets it up with your permission'
        Write-Host '  (/setup). From the shell, pick where the model runs:'
        Write-Host ''
        Write-Host '    local model (llama.cpp; the default model is ~22 GB):  bcode setup'
        Write-Host '    cloud model API (no GPU needed; your code is sent to the provider):'
        Write-Host '      bcode provider use NAME --model MODEL   # openai, anthropic, gemini, openai-compatible'
        Write-Host '      bcode provider key set NAME'
        Write-Host '      bcode setup'
        Write-Host ''
        Write-Host '  bcode setup --check lists what is still missing. Windows support is experimental.'
        Write-Host "  Uninstall: delete $BinDir and remove it from your user PATH; data is under"
        Write-Host '  %USERPROFILE%\.config, .local\share, .local\state and .cache in boundedcode folders,'
        Write-Host '  and the sandbox image (docker rmi boundedcode-openhands:local).'
    } catch {
        Write-Host "✘ $($_.Exception.Message)" -ForegroundColor Red
        $failed = $true
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }
    # A non-zero exit code only when run as a script file; under `irm | iex`
    # exiting would close the user's PowerShell session.
    if ($failed -and $PSCommandPath) { exit 1 }
}
