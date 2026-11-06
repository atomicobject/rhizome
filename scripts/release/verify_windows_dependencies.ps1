param(
    [Parameter(Mandatory = $true)][string]$Path,
    [Parameter(Mandatory = $true)][string]$Imports
)
$ErrorActionPreference = 'Stop'
Add-Type @'
using System;
using System.Runtime.InteropServices;
using System.Text;
public static class RhizomeSystemDll {
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    public static extern IntPtr LoadLibraryEx(string name, IntPtr file, uint flags);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    public static extern uint GetModuleFileName(IntPtr module, StringBuilder filename, uint size);
    [DllImport("kernel32.dll")]
    public static extern bool FreeLibrary(IntPtr module);
}
'@
$system = [Environment]::SystemDirectory
$os = Get-CimInstance Win32_OperatingSystem
if ([version]$os.Version -lt [version]'10.0.22000' -or -not [Environment]::Is64BitOperatingSystem) {
    throw 'Dependency evidence requires an x64 Windows host at or above the Windows 11 baseline.'
}
$checked = @()
foreach ($name in $Imports.Split(',')) {
    if ($name -cnotmatch '^[a-z0-9_.+-]+\.dll$') { throw 'Invalid CLI DLL import name.' }
    # The loader resolves API-set contracts too. It must not search MinGW or user PATH.
    $module = [RhizomeSystemDll]::LoadLibraryEx($name, [IntPtr]::Zero, 0x00000800)
    if ($module -eq [IntPtr]::Zero) { throw "CLI DLL is unavailable from System32: $name" }
    try {
        $buffer = New-Object System.Text.StringBuilder 32768
        $length = [RhizomeSystemDll]::GetModuleFileName($module, $buffer, $buffer.Capacity)
        if ($length -eq 0 -or $length -ge $buffer.Capacity) { throw 'Cannot identify the loaded system DLL.' }
        $resolved = $buffer.ToString()
        if (-not $resolved.StartsWith($system + '\', [StringComparison]::OrdinalIgnoreCase)) {
            throw "CLI DLL resolves outside System32: $name"
        }
        $signature = Get-AuthenticodeSignature -LiteralPath $resolved
        if ($signature.Status -ne 'Valid' -or $null -eq $signature.SignerCertificate -or
            $signature.SignerCertificate.Subject -notmatch 'Microsoft') {
            throw "CLI DLL is not a verified Microsoft system component: $name"
        }
        $checked += @{
            name = $name; path = $resolved; origin = 'system'; signature = 'microsoft-valid'
            publisher = $signature.SignerCertificate.Subject
            sha256 = (Get-FileHash -LiteralPath $resolved -Algorithm SHA256).Hash.ToLowerInvariant()
        }
    } finally {
        [void][RhizomeSystemDll]::FreeLibrary($module)
    }
}
$directory = Join-Path ([IO.Path]::GetTempPath()) ('rhizome-cli-dependencies-' + [Guid]::NewGuid().ToString('N'))
$oldPath = $env:PATH
try {
    New-Item -ItemType Directory -Path $directory | Out-Null
    $probe = Join-Path $directory 'rzm.exe'
    Copy-Item -LiteralPath $Path -Destination $probe
    $env:PATH = $system
    Push-Location $directory
    try {
        $output = @(& $probe --version 2>&1)
        if ($LASTEXITCODE -ne 0) { throw 'Copied CLI cannot run without developer runtimes or install files.' }
    } finally {
        Pop-Location
    }
} finally {
    $env:PATH = $oldPath
    Remove-Item -LiteralPath $directory -Recurse -Force -ErrorAction SilentlyContinue
}
@{
    host = @{
        os = $os.Caption; version = $os.Version; build = $os.BuildNumber
        architecture = $os.OSArchitecture; systemDirectory = $system
    }
    imports = @($checked)
    probeVersion = ($output -join "`n").Trim()
} | ConvertTo-Json -Depth 5 -Compress
