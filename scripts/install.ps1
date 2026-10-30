# Install gcx for the current Windows user.
# Run: irm https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.ps1 | iex
# GCX_VERSION and GCX_INSTALL_DIR take precedence over VERSION and INSTALL_DIR.
# Set GCX_INSTALL_PS1_LIB=1 before dot-sourcing this file to load its functions.

function Get-GcxArchitecture {
    # PROCESSOR_ARCHITEW6432 reports the host when PowerShell runs under emulation.
    $arch = $env:PROCESSOR_ARCHITEW6432
    if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
    if (-not $arch) { throw 'Cannot detect the Windows architecture.' }
    switch ($arch.ToUpperInvariant()) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        default { throw "Unsupported Windows architecture: $arch. Use amd64 or arm64." }
    }
}

function Find-GcxOnPath {
    foreach ($entry in ($env:Path -split ';')) {
        if (-not $entry) { continue }
        $directory = [Environment]::ExpandEnvironmentVariables($entry.Trim('"'))
        $candidate = Join-Path $directory 'gcx.exe'
        if (Test-Path -LiteralPath $candidate -PathType Leaf) { return [IO.Path]::GetFullPath($candidate) }
    }
    return $null
}

function Get-GcxLatestVersion {
    $headers = @{}
    if ($env:GITHUB_TOKEN) { $headers['Authorization'] = "Bearer $($env:GITHUB_TOKEN)" }
    try {
        $release = Invoke-RestMethod -Uri 'https://api.github.com/repos/grafana/gcx/releases/latest' -Headers $headers -ErrorAction Stop
    } catch {
        throw 'Cannot read the latest gcx release from the GitHub API. Set GCX_VERSION to use a known release.'
    }
    if (-not $release.tag_name) { throw 'The latest release has no tag_name.' }
    return [string]$release.tag_name
}

function Get-GcxUserPath([string]$subKey = 'Environment') {
    $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($subKey)
    if (-not $key) { return '' }
    try {
        # Keep references such as %USERPROFILE% in the stored PATH.
        return $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    } finally {
        $key.Dispose()
    }
}

function Set-GcxUserPath([string]$value, [string]$subKey = 'Environment') {
    $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($subKey)
    try {
        $key.SetValue('Path', $value, [Microsoft.Win32.RegistryValueKind]::ExpandString)
    } finally {
        $key.Dispose()
    }
}

function Send-GcxEnvironmentChange {
    $notified = $false
    try {
        if (-not ('Gcx.WindowsEnvironment' -as [type])) {
            Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
namespace Gcx {
    public static class WindowsEnvironment {
        [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern IntPtr SendMessageTimeout(
            IntPtr window, uint message, UIntPtr wParam, string lParam,
            uint flags, uint timeout, out UIntPtr result);

        public static bool Notify() {
            UIntPtr result;
            // Notify all top-level windows. Stop waiting for a window after one second.
            return SendMessageTimeout(new IntPtr(0xffff), 0x001a, UIntPtr.Zero,
                "Environment", 0x0002, 1000, out result) != IntPtr.Zero;
        }
    }
}
'@ -ErrorAction Stop
        }
        $notified = [Gcx.WindowsEnvironment]::Notify()
    } catch {
        # A notification failure must not undo the completed PATH update.
    }
    if (-not $notified) {
        Write-Warning 'The user PATH changed, but Windows did not confirm the environment update. Sign out and sign in again if new terminals cannot find gcx.' -WarningAction Continue
    }
}

function Test-GcxPathEntry([string]$pathValue, [string]$directory) {
    $wanted = [IO.Path]::GetFullPath($directory).TrimEnd('\', '/')
    foreach ($entry in ($pathValue -split ';')) {
        if (-not $entry) { continue }
        $expanded = [Environment]::ExpandEnvironmentVariables($entry.Trim('"'))
        try { $candidate = [IO.Path]::GetFullPath($expanded).TrimEnd('\', '/') }
        catch { continue }
        if ([string]::Equals($candidate, $wanted, [StringComparison]::OrdinalIgnoreCase)) { return $true }
    }
    return $false
}

function Add-GcxToPath([string]$directory) {
    $userPath = Get-GcxUserPath
    if (-not (Test-GcxPathEntry $userPath $directory)) {
        $newPath = if ($userPath) { "$($userPath.TrimEnd(';'));$directory" } else { $directory }
        Set-GcxUserPath $newPath
        Send-GcxEnvironmentChange
    }
    if (-not (Test-GcxPathEntry $env:Path $directory)) {
        $env:Path = if ($env:Path) { "$($env:Path.TrimEnd(';'));$directory" } else { $directory }
    }
}

function Get-GcxExpectedHash([string]$checksumsFile, [string]$archiveName) {
    $matchesForArchive = @()
    foreach ($line in [IO.File]::ReadAllLines($checksumsFile)) {
        if ($line -notmatch '^([0-9a-fA-F]{64})\s+\*?(.+?)\s*$') { continue }
        if ([string]::Equals($Matches[2], $archiveName, [StringComparison]::Ordinal)) {
            $matchesForArchive += $Matches[1]
        }
    }
    if ($matchesForArchive.Count -ne 1) {
        throw "Expected one SHA-256 entry for $archiveName in the checksums file."
    }
    return $matchesForArchive[0]
}

function Save-GcxFile([string]$uri, [string]$path) {
    try {
        Invoke-WebRequest -Uri $uri -OutFile $path -UseBasicParsing -ErrorAction Stop | Out-Null
    } catch {
        throw "Cannot download $uri. $($_.Exception.Message)"
    }
}

function Install-Gcx {
    $ErrorActionPreference = 'Stop'
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        throw 'This installer supports Windows only.'
    }
    $arch = Get-GcxArchitecture
    $requestedVersion = if ($env:GCX_VERSION) { $env:GCX_VERSION } elseif ($env:VERSION) { $env:VERSION } else { Get-GcxLatestVersion }
    $version = $requestedVersion -replace '^v', ''
    if ($version -notmatch '^[0-9A-Za-z][0-9A-Za-z._+-]*$') { throw "Invalid gcx version: $requestedVersion" }

    $installDir = if ($env:GCX_INSTALL_DIR) { $env:GCX_INSTALL_DIR } elseif ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'gcx\bin' }
    if (-not $installDir) { throw 'Cannot find an install directory. Set GCX_INSTALL_DIR.' }
    $installDir = [IO.Path]::GetFullPath($installDir)
    $archive = "gcx_${version}_windows_${arch}.zip"
    $checksums = "gcx_${version}_checksums.txt"
    $baseUrl = "https://github.com/grafana/gcx/releases/download/v$version"
    $workDir = Join-Path ([IO.Path]::GetTempPath()) ("gcx-install-" + [Guid]::NewGuid().ToString('N'))
    $stage = $null
    $backup = $null
    $target = Join-Path $installDir 'gcx.exe'
    $replaced = $false
    $installed = $false
    $keepBackup = $false

    try {
        New-Item -ItemType Directory -Path $workDir -ErrorAction Stop | Out-Null
        $archivePath = Join-Path $workDir $archive
        $checksumsPath = Join-Path $workDir $checksums
        Save-GcxFile "$baseUrl/$archive" $archivePath
        Save-GcxFile "$baseUrl/$checksums" $checksumsPath
        $expected = Get-GcxExpectedHash $checksumsPath $archive
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath -ErrorAction Stop).Hash
        if (-not [string]::Equals($expected, $actual, [StringComparison]::OrdinalIgnoreCase)) {
            throw "SHA-256 mismatch for $archive. Expected $expected, got $actual."
        }

        $extractDir = Join-Path $workDir 'extracted'
        Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir -ErrorAction Stop
        $source = Join-Path $extractDir 'gcx.exe'
        if (-not (Test-Path -LiteralPath $source -PathType Leaf)) { throw "$archive does not contain gcx.exe at its root." }

        New-Item -ItemType Directory -Path $installDir -Force -ErrorAction Stop | Out-Null
        $stage = Join-Path $installDir (".gcx-" + [Guid]::NewGuid().ToString('N') + '.exe')
        Copy-Item -LiteralPath $source -Destination $stage -ErrorAction Stop
        # Run the staged binary before the current installation changes.
        $versionOutput = & $stage --version
        if ($LASTEXITCODE -ne 0 -or -not $versionOutput) {
            throw "The gcx.exe in $archive did not pass the --version check."
        }
        if (Test-Path -LiteralPath $target -PathType Leaf) {
            $backup = Join-Path $installDir (".gcx-backup-" + [Guid]::NewGuid().ToString('N') + '.exe')
            [IO.File]::Replace($stage, $target, $backup)
            $replaced = $true
        } else {
            Move-Item -LiteralPath $stage -Destination $target -ErrorAction Stop
        }
        $installed = $true
        Add-GcxToPath $installDir
        Write-Host "Installed gcx $version to $target"
        $resolved = Find-GcxOnPath
        if ($resolved -and -not [string]::Equals($resolved, $target, [StringComparison]::OrdinalIgnoreCase)) {
            Write-Warning "PATH finds $resolved before $target. Use the installed path directly or move $installDir earlier in PATH."
        }
    } catch {
        $failure = $_
        if ($installed) {
            if ($replaced -and $backup -and (Test-Path -LiteralPath $backup)) {
                try { [IO.File]::Replace($backup, $target, [NullString]::Value) }
                catch {
                    $keepBackup = $true
                    throw "Install failed: $failure. Could not restore the old gcx.exe: $($_.Exception.Message). The old executable is at $backup."
                }
            } elseif (-not $replaced) {
                Remove-Item -LiteralPath $target -Force -ErrorAction SilentlyContinue
            }
        }
        throw $failure
    } finally {
        if ($stage -and (Test-Path -LiteralPath $stage)) { Remove-Item -LiteralPath $stage -Force -ErrorAction SilentlyContinue }
        if (-not $keepBackup -and $backup -and (Test-Path -LiteralPath $backup)) { Remove-Item -LiteralPath $backup -Force -ErrorAction SilentlyContinue }
        if (Test-Path -LiteralPath $workDir) { Remove-Item -LiteralPath $workDir -Recurse -Force -ErrorAction SilentlyContinue }
    }
}

if ($env:GCX_INSTALL_PS1_LIB -ne '1') { Install-Gcx }
