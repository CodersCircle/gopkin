# Gopkin CLI Installer for Windows PowerShell
$ErrorActionPreference = "Stop"

$RepoUrl = "https://github.com/CodersCircle/gopkin"
$ArchiveUrl = "$RepoUrl/archive/refs/heads/main.zip"
$InstallDir = if ($env:GOPKIN_INSTALL_DIR) { $env:GOPKIN_INSTALL_DIR } else { Join-Path $env:USERPROFILE ".gopkin\bin" }

Write-Host "Installing Gopkin CLI..." -ForegroundColor Cyan

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "Go is required to build Gopkin (zero CGO, zero external deps). Install Go from https://go.dev/dl/ and re-run."
    exit 1
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("gopkin-install-" + [System.Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null

try {
    $ZipPath = Join-Path $TmpDir "gopkin.zip"
    Write-Host "Downloading Gopkin source from $RepoUrl..."
    Invoke-WebRequest -Uri $ArchiveUrl -OutFile $ZipPath
    Expand-Archive -Path $ZipPath -DestinationPath $TmpDir -Force

    $SrcDir = Get-ChildItem -Path $TmpDir -Directory -Filter "gopkin-*" | Select-Object -First 1
    if (-not $SrcDir) {
        throw "Failed to locate extracted Gopkin source directory."
    }

    Write-Host "Building static zero-CGO gopkin.exe..."
    Push-Location $SrcDir.FullName
    try {
        $env:CGO_ENABLED = "0"
        $BinPath = Join-Path $InstallDir "gopkin.exe"
        & go build -o $BinPath ./cmd/gopkin
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed with exit code $LASTEXITCODE"
        }
    } finally {
        Pop-Location
    }

    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (-not ($UserPath -split ";" | Where-Object { $_ -eq $InstallDir })) {
        $NewPath = if ($UserPath) { "$UserPath;$InstallDir" } else { $InstallDir }
        [Environment]::SetEnvironmentVariable("Path", $NewPath, "User")
        $env:Path = "$env:Path;$InstallDir"
        Write-Host "Added $InstallDir to your User PATH." -ForegroundColor Green
    }

    Write-Host ""
    Write-Host "Gopkin installed successfully at: $(Join-Path $InstallDir 'gopkin.exe')" -ForegroundColor Green
    Write-Host "Get started:"
    Write-Host "  gopkin new MyApp; cd MyApp; gopkin run"
} finally {
    Remove-Item -Path $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
}
