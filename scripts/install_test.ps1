# Offline tests for install.ps1. Run: & ./scripts/install_test.ps1
$ErrorActionPreference = 'Stop'
$env:GCX_INSTALL_PS1_LIB = '1'
. (Join-Path $PSScriptRoot 'install.ps1')
Remove-Item Env:GCX_INSTALL_PS1_LIB

$script:passed = 0
$script:failed = 0
$script:fixtureDir = Join-Path ([IO.Path]::GetTempPath()) ("gcx fixture " + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $script:fixtureDir | Out-Null
$script:fixtureExe = Join-Path $script:fixtureDir 'fixture.exe'
$fixtureSource = Join-Path $script:fixtureDir 'main.go'
[IO.File]::WriteAllText($fixtureSource, @'
package main
import (
    "fmt"
    "os"
)
func main() {
    if len(os.Args) == 2 && os.Args[1] == "--version" {
        fmt.Println("gcx version 1.2.3")
        return
    }
    os.Exit(2)
}
'@)
& go build -o $script:fixtureExe $fixtureSource
if ($LASTEXITCODE -ne 0) { throw 'Cannot build the Windows test executable. Install Go before running these tests.' }
$script:arch = Get-GcxArchitecture
$script:version = '1.2.3'
$script:archive = "gcx_$($script:version)_windows_$($script:arch).zip"
$script:checksums = "gcx_$($script:version)_checksums.txt"
$script:originalEnv = @{}
foreach ($name in @('GCX_VERSION', 'VERSION', 'GCX_INSTALL_DIR', 'INSTALL_DIR', 'LOCALAPPDATA', 'Path')) {
    $script:originalEnv[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

function Assert-Gcx([bool]$condition, [string]$message) {
    if (-not $condition) { throw $message }
}

function Assert-GcxThrows([scriptblock]$action, [string]$message) {
    $threw = $false
    try { & $action } catch { $threw = $true }
    Assert-Gcx $threw $message
}

# Keep the real helpers for the isolated registry test.
$script:readUserPath = ${function:Get-GcxUserPath}
$script:writeUserPath = ${function:Set-GcxUserPath}

function Get-GcxUserPath { return $script:userPath }
function Set-GcxUserPath([string]$value) {
    if ($script:failPathUpdate) { throw 'Test PATH update failure.' }
    $script:userPath = $value
}
# Installer cases must not notify other applications on the test machine.
function Send-GcxEnvironmentChange {}
function Get-GcxLatestVersion { return 'v1.2.3' }
function Save-GcxFile([string]$uri, [string]$path) {
    if ($script:failDownload) { throw 'Test download failure.' }
    $name = $uri.Substring($uri.LastIndexOf('/') + 1)
    Copy-Item -LiteralPath (Join-Path $script:assetDir $name) -Destination $path -ErrorAction Stop
}

function New-GcxFixture {
    $script:caseDir = Join-Path ([IO.Path]::GetTempPath()) ("gcx install test " + [Guid]::NewGuid().ToString('N'))
    $script:assetDir = Join-Path $script:caseDir 'release files'
    $script:installDir = Join-Path $script:caseDir 'program files\gcx bin'
    New-Item -ItemType Directory -Path $script:assetDir -Force | Out-Null
    $payloadDir = Join-Path $script:caseDir 'payload'
    New-Item -ItemType Directory -Path $payloadDir | Out-Null
    Copy-Item -LiteralPath $script:fixtureExe -Destination (Join-Path $payloadDir 'gcx.exe')
    Compress-Archive -LiteralPath (Join-Path $payloadDir 'gcx.exe') -DestinationPath (Join-Path $script:assetDir $script:archive)
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $script:assetDir $script:archive)).Hash
    [IO.File]::WriteAllText((Join-Path $script:assetDir $script:checksums), "$hash  $($script:archive)`n")
    $script:userPath = 'C:\Other Tools'
    $script:failPathUpdate = $false
    $script:failDownload = $false
    $env:GCX_VERSION = $script:version
    Remove-Item Env:VERSION -ErrorAction SilentlyContinue
    $env:GCX_INSTALL_DIR = $script:installDir
    Remove-Item Env:INSTALL_DIR -ErrorAction SilentlyContinue
    $env:Path = 'C:\Other Tools'
}

function Invoke-GcxCase([string]$name, [scriptblock]$body) {
    try {
        New-GcxFixture
        & $body
        Write-Host "PASS $name"
        $script:passed++
    } catch {
        Write-Host "FAIL $name`: $($_.Exception.Message)" -ForegroundColor Red
        $script:failed++
    } finally {
        if ($script:caseDir -and (Test-Path -LiteralPath $script:caseDir)) {
            Remove-Item -LiteralPath $script:caseDir -Recurse -Force
        }
        foreach ($key in $script:originalEnv.Keys) {
            [Environment]::SetEnvironmentVariable($key, $script:originalEnv[$key], 'Process')
        }
    }
}

Invoke-GcxCase 'preserve raw PATH references and the expandable registry type' {
    $subKey = 'Software\gcx-install-test-' + [Guid]::NewGuid().ToString('N')
    $key = $null
    try {
        $key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($subKey)
        $rawPath = '%USERPROFILE%\AppData\Local\Microsoft\WindowsApps;%TEMP%\Tools'
        $key.SetValue('Path', $rawPath, [Microsoft.Win32.RegistryValueKind]::ExpandString)
        $readPath = & $script:readUserPath $subKey
        Assert-Gcx ($readPath -ceq $rawPath) 'The registry read expanded a PATH reference.'
        $newPath = "$readPath;$($script:installDir)"
        & $script:writeUserPath $newPath $subKey
        $storedPath = $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        Assert-Gcx ($storedPath -ceq $newPath) 'The registry write changed a raw PATH reference.'
        Assert-Gcx ($key.GetValueKind('Path') -eq [Microsoft.Win32.RegistryValueKind]::ExpandString) 'The PATH registry type is not REG_EXPAND_SZ.'
    } finally {
        if ($key) { $key.Dispose() }
        [Microsoft.Win32.Registry]::CurrentUser.DeleteSubKeyTree($subKey, $false)
    }
}

Invoke-GcxCase 'keep raw PATH references when adding the install directory' {
    $script:userPath = '%USERPROFILE%\AppData\Local\Microsoft\WindowsApps;%TEMP%\Tools'
    $rawPath = $script:userPath
    Add-GcxToPath $script:installDir
    Assert-Gcx ($script:userPath -ceq "$rawPath;$($script:installDir)") 'The PATH update changed an existing reference.'
}

Invoke-GcxCase 'install in a path with spaces and run the executable' {
    Install-Gcx
    $target = Join-Path $script:installDir 'gcx.exe'
    Assert-Gcx (Test-Path -LiteralPath $target -PathType Leaf) 'gcx.exe is missing.'
    & $target --version | Out-Null
    Assert-Gcx ($LASTEXITCODE -eq 0) 'The installed executable did not run.'
    Assert-Gcx (Test-GcxPathEntry $script:userPath $script:installDir) 'The user PATH was not updated.'
    Assert-Gcx (Test-GcxPathEntry $env:Path $script:installDir) 'The process PATH was not updated.'
    Install-Gcx
    $userEntries = @(($script:userPath -split ';') | Where-Object { $_ -eq $script:installDir })
    $processEntries = @(($env:Path -split ';') | Where-Object { $_ -eq $script:installDir })
    Assert-Gcx ($userEntries.Count -eq 1 -and $processEntries.Count -eq 1) 'The installer added a duplicate PATH entry.'
}

Invoke-GcxCase 'upgrade an existing executable' {
    New-Item -ItemType Directory -Path $script:installDir -Force | Out-Null
    $target = Join-Path $script:installDir 'gcx.exe'
    [IO.File]::WriteAllText($target, 'old gcx')
    Install-Gcx
    $wanted = (Get-FileHash -Algorithm SHA256 -LiteralPath $script:fixtureExe).Hash
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $target).Hash
    Assert-Gcx ($actual -eq $wanted) 'The upgrade did not replace the executable.'
    Assert-Gcx (@(Get-ChildItem -LiteralPath $script:installDir -Filter '.gcx-backup-*').Count -eq 0) 'A backup was not removed.'
}

Invoke-GcxCase 'use version and directory overrides in precedence order' {
    $env:VERSION = '9.9.9'
    $env:INSTALL_DIR = Join-Path $script:caseDir 'legacy bin'
    Install-Gcx
    Assert-Gcx (Test-Path -LiteralPath (Join-Path $script:installDir 'gcx.exe')) 'GCX_INSTALL_DIR did not win.'
    Remove-Item Env:GCX_VERSION
    Remove-Item Env:GCX_INSTALL_DIR
    $env:VERSION = $script:version
    Install-Gcx
    Assert-Gcx (Test-Path -LiteralPath (Join-Path $env:INSTALL_DIR 'gcx.exe')) 'Legacy INSTALL_DIR did not work.'
    Remove-Item Env:VERSION
    Remove-Item Env:INSTALL_DIR
    $env:LOCALAPPDATA = Join-Path $script:caseDir 'local app data'
    Install-Gcx
    Assert-Gcx (Test-Path -LiteralPath (Join-Path $env:LOCALAPPDATA 'gcx\bin\gcx.exe')) 'The default directory is wrong.'
}

Invoke-GcxCase 'keep the old executable after a checksum mismatch' {
    New-Item -ItemType Directory -Path $script:installDir -Force | Out-Null
    $target = Join-Path $script:installDir 'gcx.exe'
    [IO.File]::WriteAllText($target, 'old gcx')
    [IO.File]::WriteAllText((Join-Path $script:assetDir $script:checksums), (('0' * 64) + "  $($script:archive)`n"))
    Assert-GcxThrows { Install-Gcx } 'A bad SHA-256 hash was accepted.'
    Assert-Gcx ([IO.File]::ReadAllText($target) -eq 'old gcx') 'The old executable changed after a checksum failure.'
}

Invoke-GcxCase 'require an exact checksum filename' {
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $script:assetDir $script:archive)).Hash
    [IO.File]::WriteAllText((Join-Path $script:assetDir $script:checksums), "$hash  $($script:archive).bak`n")
    Assert-GcxThrows { Install-Gcx } 'A checksum for another filename was accepted.'
    Assert-Gcx (-not (Test-Path -LiteralPath (Join-Path $script:installDir 'gcx.exe'))) 'An unverified executable was installed.'
}

Invoke-GcxCase 'reject duplicate checksum entries' {
    $checksumPath = Join-Path $script:assetDir $script:checksums
    $entry = [IO.File]::ReadAllText($checksumPath)
    [IO.File]::WriteAllText($checksumPath, $entry + $entry)
    Assert-GcxThrows { Install-Gcx } 'Duplicate checksum entries were accepted.'
}

Invoke-GcxCase 'keep the old executable when the archive cannot run' {
    New-Item -ItemType Directory -Path $script:installDir -Force | Out-Null
    $target = Join-Path $script:installDir 'gcx.exe'
    [IO.File]::WriteAllText($target, 'old gcx')
    $badDir = Join-Path $script:caseDir 'bad payload'
    New-Item -ItemType Directory -Path $badDir | Out-Null
    [IO.File]::WriteAllText((Join-Path $badDir 'gcx.exe'), 'not an executable')
    $archivePath = Join-Path $script:assetDir $script:archive
    Remove-Item -LiteralPath $archivePath
    Compress-Archive -LiteralPath (Join-Path $badDir 'gcx.exe') -DestinationPath $archivePath
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash
    [IO.File]::WriteAllText((Join-Path $script:assetDir $script:checksums), "$hash  $($script:archive)`n")
    $ErrorActionPreference = 'Continue'
    $global:LASTEXITCODE = 0
    Assert-GcxThrows { Install-Gcx } 'A non-executable gcx.exe was installed.'
    Assert-Gcx ([IO.File]::ReadAllText($target) -eq 'old gcx') 'The old executable changed after validation failed.'
}

Invoke-GcxCase 'restore the old executable when the PATH update fails' {
    New-Item -ItemType Directory -Path $script:installDir -Force | Out-Null
    $target = Join-Path $script:installDir 'gcx.exe'
    [IO.File]::WriteAllText($target, 'old gcx')
    $script:failPathUpdate = $true
    Assert-GcxThrows { Install-Gcx } 'A failed PATH update was ignored.'
    Assert-Gcx ([IO.File]::ReadAllText($target) -eq 'old gcx') 'The old executable was not restored.'
}

Invoke-GcxCase 'fail on a download error and keep the old executable' {
    New-Item -ItemType Directory -Path $script:installDir -Force | Out-Null
    $target = Join-Path $script:installDir 'gcx.exe'
    [IO.File]::WriteAllText($target, 'old gcx')
    $script:failDownload = $true
    Assert-GcxThrows { Install-Gcx } 'A download failure was ignored.'
    Assert-Gcx ([IO.File]::ReadAllText($target) -eq 'old gcx') 'The old executable changed after a download failure.'
}

Invoke-GcxCase 'reject an invalid version before download' {
    $env:GCX_VERSION = '../escape'
    Assert-GcxThrows { Install-Gcx } 'An invalid version was accepted.'
}

Invoke-GcxCase 'reject an unsupported Windows architecture' {
    $originalArch = $env:PROCESSOR_ARCHITECTURE
    $originalHostArch = $env:PROCESSOR_ARCHITEW6432
    try {
        $env:PROCESSOR_ARCHITECTURE = 'MIPS'
        Remove-Item Env:PROCESSOR_ARCHITEW6432 -ErrorAction SilentlyContinue
        Assert-GcxThrows { Install-Gcx } 'An unsupported architecture was accepted.'
    } finally {
        [Environment]::SetEnvironmentVariable('PROCESSOR_ARCHITECTURE', $originalArch, 'Process')
        [Environment]::SetEnvironmentVariable('PROCESSOR_ARCHITEW6432', $originalHostArch, 'Process')
    }
}

Write-Host "$($script:passed) passed, $($script:failed) failed"
Remove-Item -LiteralPath $script:fixtureDir -Recurse -Force
if ($script:failed -gt 0) { exit 1 }
