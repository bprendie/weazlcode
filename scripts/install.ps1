param(
    [switch]$SkipLaunch,
    [switch]$SkipSetup
)

$ErrorActionPreference = "Stop"

$AppName = "weazlcode"
$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")

if ($env:WEAZLCODE_HOME) {
    $InstallRoot = $env:WEAZLCODE_HOME
} elseif ($env:APPDATA) {
    $InstallRoot = Join-Path $env:APPDATA $AppName
} else {
    $InstallRoot = Join-Path $HOME "AppData\Roaming\$AppName"
}

$BinDir = Join-Path $InstallRoot "bin"
$ConfigDir = Join-Path $InstallRoot "config"
$VaultDir = Join-Path $InstallRoot "vaults"
$CacheDir = Join-Path $InstallRoot "cache"
$GoCache = if ($env:GOCACHE) { $env:GOCACHE } else { Join-Path $CacheDir "go-build" }
$GoModCache = if ($env:GOMODCACHE) { $env:GOMODCACHE } else { Join-Path $CacheDir "go-mod" }
$ExePath = Join-Path $BinDir "$AppName.exe"
$MsysRoot = if ($env:MSYS2_ROOT) { $env:MSYS2_ROOT } else { "C:\msys64" }
$MsysBash = Join-Path $MsysRoot "usr\bin\bash.exe"
$MsysUcrtBin = Join-Path $MsysRoot "ucrt64\bin"

function Refresh-SessionPath {
    $machinePath = [Environment]::GetEnvironmentVariable("Path", "Machine")
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $extra = @(
        $BinDir,
        $MsysUcrtBin,
        "C:\Program Files\Go\bin",
        (Join-Path $HOME "go\bin")
    ) | Where-Object { $_ -and (Test-Path $_) }
    $env:Path = (@($extra) + @($userPath, $machinePath, $env:Path)) -join ";"
}

function Add-UserPath {
    param([Parameter(Mandatory = $true)][string]$PathToAdd)
    $current = [Environment]::GetEnvironmentVariable("Path", "User")
    $parts = @()
    if ($current) {
        $parts = $current -split ";" | Where-Object { $_ }
    }
    $alreadyPresent = $false
    foreach ($part in $parts) {
        if ($part.TrimEnd([char]92) -ieq $PathToAdd.TrimEnd([char]92)) {
            $alreadyPresent = $true
            break
        }
    }
    if (-not $alreadyPresent) {
        $newPath = (@($parts) + $PathToAdd) -join ";"
        [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
        Write-Host "Added $PathToAdd to your user PATH."
    }
}

function Require-Winget {
    if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
        throw "winget is required to install missing dependencies automatically. Install App Installer from the Microsoft Store, then rerun scripts\install.ps1."
    }
}

function Ensure-Go {
    Refresh-SessionPath
    if (Get-Command go -ErrorAction SilentlyContinue) {
        return
    }
    Require-Winget
    Write-Host "Installing Go with winget..."
    winget install --id GoLang.Go --source winget --accept-package-agreements --accept-source-agreements
    Refresh-SessionPath
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "Go was installed but is not visible on PATH yet. Open a new PowerShell window and rerun scripts\install.ps1."
    }
}

function Ensure-Git {
    Refresh-SessionPath
    if (Get-Command git -ErrorAction SilentlyContinue) {
        return
    }
    Require-Winget
    Write-Host "Installing Git with winget..."
    winget install --id Git.Git --source winget --accept-package-agreements --accept-source-agreements
    Refresh-SessionPath
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
        throw "Git was installed but is not visible on PATH yet. Open a new PowerShell window and rerun scripts\install.ps1."
    }
}

function Ensure-Msys2Gcc {
    Refresh-SessionPath
    if (Get-Command gcc -ErrorAction SilentlyContinue) {
        return
    }
    if (-not (Test-Path $MsysBash)) {
        Require-Winget
        Write-Host "Installing MSYS2 with winget..."
        winget install --id MSYS2.MSYS2 --source winget --accept-package-agreements --accept-source-agreements
    }
    if (-not (Test-Path $MsysBash)) {
        throw "MSYS2 bash was not found at $MsysBash. Install MSYS2 or set MSYS2_ROOT, then rerun scripts\install.ps1."
    }
    Write-Host "Installing MSYS2 UCRT64 GCC toolchain..."
    & $MsysBash -lc "pacman -Syu --noconfirm"
    & $MsysBash -lc "pacman -S --needed --noconfirm mingw-w64-ucrt-x86_64-gcc"
    Add-UserPath $MsysUcrtBin
    Refresh-SessionPath
    if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
        throw "GCC was installed but is not visible on PATH. Open a new PowerShell window and rerun scripts\install.ps1."
    }
}

New-Item -ItemType Directory -Force -Path $BinDir, $ConfigDir, $VaultDir, $GoCache, $GoModCache | Out-Null

Ensure-Go
Ensure-Git
Ensure-Msys2Gcc
Add-UserPath $BinDir
Refresh-SessionPath

Write-Host "Building $AppName..."
Push-Location $RepoRoot
try {
    $env:CGO_ENABLED = "1"
    $env:GOCACHE = $GoCache
    $env:GOMODCACHE = $GoModCache
    go build -buildvcs=false -o $ExePath .\cmd\weazlcode
} finally {
    Pop-Location
}

Write-Host "Installed $AppName to $ExePath"
Write-Host "Config: $ConfigDir\config.json"
Write-Host "Vaults: $VaultDir"
Write-Host "If PowerShell cannot find $AppName yet, open a new terminal or run:"
Write-Host "  `$env:Path = `"$BinDir;`$env:Path`""

$SkipSetupEnv = $env:WEAZLCODE_SKIP_SETUP -eq "1"
$SkipLaunchEnv = $env:WEAZLCODE_SKIP_LAUNCH -eq "1"

if (-not $SkipSetup -and -not $SkipSetupEnv) {
    Write-Host ""
    Write-Host "Configuring provider and optional tools..."
    Push-Location $RepoRoot
    try {
        go run -buildvcs=false .\cmd\weazlcode-setup
    } finally {
        Pop-Location
    }
}

if ($SkipLaunch -or $SkipLaunchEnv) {
    Write-Host "Skipping first launch."
} else {
    Write-Host ""
    Write-Host "Launching $AppName..."
    & $ExePath
}
