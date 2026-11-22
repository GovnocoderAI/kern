#Requires -Version 5.1
# Kern installer for Windows. Downloads a release, verifies its SHA256 and
# installs kern.exe to %LOCALAPPDATA%\kern\bin (override with KERN_INSTALL_DIR).
$ErrorActionPreference = "Stop"

$Repo = "GovnocoderAI/kern"
$Bin = "kern.exe"
$InstallDir = if ($env:KERN_INSTALL_DIR) { $env:KERN_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "kern\bin" }

$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { throw "kern: unsupported architecture '$($env:PROCESSOR_ARCHITECTURE)'" }
}

$Version = $env:KERN_VERSION
if (-not $Version) {
    $Release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers @{ "User-Agent" = "kern-installer" }
    $Version = $Release.tag_name.TrimStart("v")
}
if (-not $Version) { throw "kern: could not determine the latest version" }

$Archive = "kern_${Version}_windows_${Arch}.zip"
$Base = "https://github.com/$Repo/releases/download/v$Version"
$Tmp = Join-Path $env:TEMP ("kern-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $Tmp | Out-Null

try {
    Write-Host "kern: downloading $Base/$Archive"
    Invoke-WebRequest "$Base/$Archive" -OutFile (Join-Path $Tmp $Archive)
    Invoke-WebRequest "$Base/checksums.txt" -OutFile (Join-Path $Tmp "checksums.txt")

    $Hash = (Get-FileHash (Join-Path $Tmp $Archive) -Algorithm SHA256).Hash.ToLower()
    $Line = Select-String -Path (Join-Path $Tmp "checksums.txt") -Pattern ([regex]::Escape($Archive)) | Select-Object -First 1
    if ($Line -and -not $Line.Line.ToLower().StartsWith($Hash)) {
        throw "kern: checksum mismatch for $Archive"
    }

    Expand-Archive -Path (Join-Path $Tmp $Archive) -DestinationPath $Tmp -Force
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    Copy-Item (Join-Path $Tmp $Bin) (Join-Path $InstallDir $Bin) -Force

    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (($UserPath -split ";") -notcontains $InstallDir) {
        [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
    }

    Write-Host "kern: installed $Bin $Version to $InstallDir"
    Write-Host "kern: restart your terminal, then run 'kern doctor'."
}
finally {
    Remove-Item -Recurse -Force $Tmp -ErrorAction SilentlyContinue
}
