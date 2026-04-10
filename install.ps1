#Requires -Version 5.1
<#
.SYNOPSIS
    Install clictl on Windows.
.DESCRIPTION
    Downloads and installs the latest clictl binary for Windows.
    Optionally specify an install directory as the first argument.
.EXAMPLE
    irm https://download.clictl.dev/install.ps1 | iex
.EXAMPLE
    .\install.ps1 C:\tools
#>
param(
    [string]$InstallDir = "$env:LOCALAPPDATA\clictl\bin"
)

$ErrorActionPreference = "Stop"

$Repo = "clictl/cli"
$DownloadBase = "https://download.clictl.dev"
$BinaryName = "clictl.exe"

function Write-Status($msg) { Write-Host "  $msg" -ForegroundColor Green }
function Write-Err($msg) { Write-Host "  $msg" -ForegroundColor Red }

# Banner
Write-Host ""
Write-Host "  clictl installer" -ForegroundColor Green
Write-Host ""

# Detect architecture
$arch = if ([Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
} else {
    Write-Err "32-bit Windows is not supported."
    exit 1
}

Write-Status "Detected: windows/$arch"

# Get latest release from version manifest (with GitHub API fallback)
Write-Status "Fetching latest release..."
$release = $null
try {
    $versionJson = Invoke-RestMethod -Uri "$DownloadBase/version.json" -ErrorAction Stop
    $release = $versionJson.version
} catch {}

if (-not $release) {
    Write-Status "Falling back to GitHub API..."
    try {
        $latest = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -ErrorAction Stop
        $release = $latest.tag_name
    } catch {
        Write-Err "Could not fetch latest release."
        exit 1
    }
}

Write-Status "Latest release: $release"

# Download
$filename = "clictl-windows-$arch.zip"
$downloadUrl = "$DownloadBase/releases/$release/$filename"
$tempZip = Join-Path $env:TEMP $filename

Write-Status "Downloading from $downloadUrl..."
try {
    Invoke-WebRequest -Uri $downloadUrl -OutFile $tempZip -ErrorAction Stop
} catch {
    Write-Status "Falling back to GitHub Releases..."
    $downloadUrl = "https://github.com/$Repo/releases/download/$release/$filename"
    try {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $tempZip -ErrorAction Stop
    } catch {
        Write-Err "Failed to download $filename"
        exit 1
    }
}

# Extract
Write-Status "Extracting..."
$tempExtract = Join-Path $env:TEMP "clictl-extract"
if (Test-Path $tempExtract) { Remove-Item -Recurse -Force $tempExtract }
Expand-Archive -Path $tempZip -DestinationPath $tempExtract -Force

$extractedBinary = Get-ChildItem -Path $tempExtract -Filter $BinaryName -Recurse | Select-Object -First 1
if (-not $extractedBinary) {
    Write-Err "Binary not found after extraction."
    Remove-Item -Force $tempZip
    Remove-Item -Recurse -Force $tempExtract
    exit 1
}

# Install
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

$installPath = Join-Path $InstallDir $BinaryName
Copy-Item -Path $extractedBinary.FullName -Destination $installPath -Force

# Cleanup
Remove-Item -Force $tempZip
Remove-Item -Recurse -Force $tempExtract

Write-Host ""
Write-Status "Installation successful!"
Write-Host ""
Write-Host "  Binary:  $installPath"
Write-Host "  Version: $release"
Write-Host ""

# Check if install dir is in PATH
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$InstallDir*") {
    Write-Host "  Adding $InstallDir to your PATH..." -ForegroundColor Yellow
    [Environment]::SetEnvironmentVariable(
        "Path",
        "$userPath;$InstallDir",
        "User"
    )
    $env:Path = "$env:Path;$InstallDir"
    Write-Host "  Done. Restart your terminal for PATH changes to take effect." -ForegroundColor Yellow
    Write-Host ""
}

Write-Host "  Register as an agent:" -ForegroundColor Yellow
Write-Host ""
Write-Host "    Option 1: Auto-detect and install skill file"
Write-Host "      clictl install"
Write-Host ""
Write-Host "    Option 2: Add as an MCP server"
Write-Host "      clictl install --mcp"
Write-Host ""
