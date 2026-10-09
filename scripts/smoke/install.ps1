# Installer and first-run smoke test for Windows (PowerShell 7 or Windows
# PowerShell 5.1). It runs scripts/install.ps1 into a throw-away folder,
# against a release directory served on 127.0.0.1, and checks: install
# (checksum verified), version, an idempotent re-run, refusal of a tampered
# binary, `setup --check` failing until set-up is complete,
# `setup --only config`, and the cloud-provider path skipping the
# local-model steps.
#
# usage: pwsh scripts/smoke/install.ps1 -Release DIR
#   DIR  laid out like a release: TAG\boundedcode-windows-ARCH.exe and
#        TAG\SHA256SUMS
# The user PATH is restored afterwards. Needs python (for the local server).
param([Parameter(Mandatory = $true)][string]$Release)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$root = Resolve-Path (Join-Path $PSScriptRoot '..\..')
$installer = Join-Path $root 'scripts\install.ps1'
$work = Join-Path ([IO.Path]::GetTempPath()) ("bc-smoke-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $work | Out-Null
$bin = Join-Path $work 'bin'
$tag = (Get-ChildItem $Release -Directory | Select-Object -First 1).Name
$savedPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$pass = 0
function Ok($m) { $script:pass++; Write-Host "  ok   $m" }
function Fail($m, $out) { Write-Host "  FAIL $m" -ForegroundColor Red; if ($out) { $out | ForEach-Object { Write-Host "       | $_" } }; exit 1 }

# Serve the release directory (Invoke-WebRequest does not read file:// URLs).
$port = 8765
$server = Start-Process -PassThru -WindowStyle Hidden python -ArgumentList '-m', 'http.server', $port, '--bind', '127.0.0.1', '--directory', $Release
Start-Sleep -Seconds 2
function Install($base) {
    $env:BC_BIN_DIR = $bin; $env:BC_VERSION = $tag; $env:BC_DOWNLOAD_BASE = $base
    $out = & pwsh -NoProfile -File $installer 2>&1 | Out-String
    return @{ Code = $LASTEXITCODE; Out = $out }
}
try {
    Write-Host '1. fresh install'
    $r = Install "http://127.0.0.1:$port"
    if ($r.Code -ne 0) { Fail 'install.ps1 exited non-zero' $r.Out }
    if ($r.Out -notmatch 'checksum verified') { Fail 'no checksum verification reported' $r.Out }
    foreach ($n in 'boundedcode.exe', 'bcode.exe') { if (-not (Test-Path (Join-Path $bin $n))) { Fail "$n not installed" $r.Out } }
    $v = & (Join-Path $bin 'bcode.exe') version
    if ($v -notmatch '^boundedcode ') { Fail "unexpected version output: $v" }
    Ok "installed, checksum verified: $v"

    Write-Host '2. re-run'
    $r = Install "http://127.0.0.1:$port"
    if ($r.Code -ne 0) { Fail 'second run failed' $r.Out }
    if (Get-ChildItem $bin -Filter '.*.new') { Fail 'staging files left behind' }
    Ok 'idempotent; no staging files left'

    Write-Host '3. tampered release'
    $bad = Join-Path $work 'bad'
    Copy-Item -Recurse $Release $bad
    $exe = Get-ChildItem (Join-Path $bad $tag) -Filter '*.exe' | Select-Object -First 1
    Add-Content -Path $exe.FullName -Value 'tampered'
    $before = (Get-FileHash (Join-Path $bin 'boundedcode.exe')).Hash
    Stop-Process -Id $server.Id
    $server = Start-Process -PassThru -WindowStyle Hidden python -ArgumentList '-m', 'http.server', $port, '--bind', '127.0.0.1', '--directory', $bad
    Start-Sleep -Seconds 2
    $r = Install "http://127.0.0.1:$port"
    if ($r.Code -eq 0) { Fail 'a tampered binary was installed' $r.Out }
    if ($r.Out -notmatch 'checksum mismatch') { Fail 'no checksum mismatch message' $r.Out }
    if ((Get-FileHash (Join-Path $bin 'boundedcode.exe')).Hash -ne $before) { Fail 'the installed binary changed' }
    Ok 'checksum mismatch refused; installed binary untouched'

    Write-Host '4. first run'
    $env:BOUNDEDCODE_HOME = Join-Path $work 'home'
    $env:BOUNDEDCODE_SECRETS = 'file'
    $b = Join-Path $bin 'bcode.exe'
    $out = & $b setup --check 2>&1 | Out-String
    if ($LASTEXITCODE -eq 0) { Fail 'setup --check succeeded with nothing set up' $out }
    if ($out -notmatch '\[todo\] Configuration') { Fail 'configuration not reported as missing' $out }
    Ok 'setup --check exits non-zero and lists what is missing'
    $out = & $b setup --only config --yes 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { Fail 'setup --only config' $out }
    $out = & $b setup --check 2>&1 | Out-String
    if ($out -notmatch 'provider use NAME') { Fail 'the local-model steps do not name the cloud alternative' $out }
    Ok 'configuration written; local-model steps name the cloud alternative'
    $out = & $b provider use openai-compatible --base-url http://127.0.0.1:9/v1 --model smoke --context-window 32768 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) { Fail 'provider use' $out }
    $out = & $b setup --check 2>&1 | Out-String
    if (([regex]::Matches($out, 'not needed: using')).Count -ne 2) { Fail 'inference and model steps are not skipped for a cloud provider' $out }
    Ok 'with a cloud provider, the llama.cpp and model steps are skipped'
    Write-Host "installer smoke: $pass checks passed"
} finally {
    Stop-Process -Id $server.Id -ErrorAction SilentlyContinue
    [Environment]::SetEnvironmentVariable('Path', $savedPath, 'User')
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}
