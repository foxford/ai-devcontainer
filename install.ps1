param([switch]$NoOrca, [switch]$NoShell)
$ErrorActionPreference = 'Stop'
$platform = if ($env:AI_DEVCONTAINER_HOME) { $env:AI_DEVCONTAINER_HOME } else { Join-Path $HOME '.local/share/ai-devcontainer' }
$state = if ($env:ADC_HOME) { $env:ADC_HOME } else { Join-Path $HOME '.adc' }
$repo = if ($env:AI_DEVCONTAINER_REPO) { $env:AI_DEVCONTAINER_REPO } else { 'https://github.com/foxford/ai-devcontainer.git' }

$legacy = (Test-Path (Join-Path $platform '.git')) -and (Test-Path (Join-Path $platform 'bin/adc') -PathType Leaf) -and -not (Test-Path (Join-Path $platform 'go.mod'))

if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
    $winget = Join-Path ([IO.Path]::GetTempPath()) 'adc-AppInstaller.msixbundle'
    Invoke-WebRequest 'https://aka.ms/getwinget' -OutFile $winget
    # Add-AppxPackage verifies Microsoft's package signature and requests dependencies.
    Add-AppxPackage -Path $winget
    Remove-Item $winget
}
if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    winget install --id Git.Git --exact --accept-package-agreements --accept-source-agreements
    if ($LASTEXITCODE -ne 0) { throw 'Git installation failed' }
    $env:Path = [Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [Environment]::GetEnvironmentVariable('Path','User')
}
$arch = if ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'amd64' }
$digest = if ($arch -eq 'arm64') { 'c17e09676be0faad3cbed1c81bb02f38fb73e2f93d048571cc13730fe23f2d5b' } else { '9b68112c913f45b7aebbf13c036721264bbba7e03a642f8f7490c561eebd1ecc' }
$toolchain = Join-Path $state 'toolchains/go1.26.1'
$go = Join-Path $toolchain 'go/bin/go.exe'
if (-not (Test-Path $go)) {
    $archive = Join-Path ([IO.Path]::GetTempPath()) "adc-go-$arch.zip"
    Invoke-WebRequest "https://go.dev/dl/go1.26.1.windows-$arch.zip" -OutFile $archive
    if ((Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $digest) { throw 'Go checksum mismatch' }
    Expand-Archive $archive -DestinationPath $toolchain -Force
    Remove-Item $archive
}
$env:GOCACHE = Join-Path $state 'cache/go-build'
$env:GOMODCACHE = Join-Path $state 'cache/go-mod'
if ($legacy) {
    if (-not $env:AI_DEVCONTAINER_REPO) {
        $repo = git -C $platform remote get-url origin
        if ($LASTEXITCODE -ne 0) { throw 'Legacy origin is unavailable' }
    }
    $stage = Join-Path $state ('bootstrap/legacy-' + [Guid]::NewGuid().ToString('N'))
    $source = Join-Path $stage 'platform'
    $helper = Join-Path $stage 'adc.exe'
    New-Item -ItemType Directory -Force $stage | Out-Null
    try {
        git clone --branch main $repo $source
        if ($LASTEXITCODE -ne 0) { throw 'Bootstrap clone failed' }
        Push-Location $source
        try { & $go build -trimpath -o $helper ./bin/adc; if ($LASTEXITCODE -ne 0) { throw 'Bootstrap build failed' } }
        finally { Pop-Location }
        $env:AI_DEVCONTAINER_HOME = $source
        $env:ADC_HOME = $state
        & $helper bootstrap-replace-legacy $platform
        if ($LASTEXITCODE -ne 0) { throw 'Legacy platform replacement failed; original agent data remains intact' }
    } finally {
        $env:AI_DEVCONTAINER_HOME = $platform
        Remove-Item -LiteralPath $stage -Recurse -Force
    }
} elseif (Test-Path (Join-Path $platform '.git')) {
    git -C $platform pull --ff-only
    if ($LASTEXITCODE -ne 0) { throw 'Platform update failed' }
} else {
    git clone --branch main $repo $platform
    if ($LASTEXITCODE -ne 0) { throw 'Clone failed' }
}
if (-not (Test-Path (Join-Path $platform 'go.mod')) -or -not (Test-Path (Join-Path $platform 'runtime/orca-lock.json'))) {
    throw 'Platform checkout is incomplete: go.mod or runtime/orca-lock.json missing'
}
$env:AI_DEVCONTAINER_HOME = $platform
$env:ADC_HOME = $state
$bin = Join-Path $state 'bin/adc.exe'
New-Item -ItemType Directory -Force (Split-Path $bin) | Out-Null
Push-Location $platform
try { & $go build -trimpath -o $bin ./bin/adc; if ($LASTEXITCODE -ne 0) { throw 'ADC build failed' } }
finally { Pop-Location }
$arguments = @('install')
if ($NoOrca) { $arguments += '--no-orca' }
if ($NoShell) { $arguments += '--no-shell' }
& $bin @arguments
if ($LASTEXITCODE -ne 0) { throw 'ADC install failed; see the diagnostic above' }
if (-not $NoShell) {
    $env:Path = (Split-Path $bin) + ';' + $env:Path
    $completion = Join-Path $state 'completions/adc.powershell'
    if (Test-Path $completion) { . $completion }
}
