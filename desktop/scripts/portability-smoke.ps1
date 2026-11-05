param(
    [Parameter(Mandatory = $true)][string]$AppPath,
    [Parameter(Mandatory = $true)][string]$OutputDir
)

$ErrorActionPreference = 'Stop'
$app = (Resolve-Path -LiteralPath $AppPath).ProviderPath
$output = [IO.Path]::GetFullPath($OutputDir)
$null = New-Item -ItemType Directory -Force -Path $output
$name = [IO.Path]::GetFileNameWithoutExtension($app)
if (Get-Process -Name $name -ErrorAction SilentlyContinue) {
    throw 'Another desktop app is running; refusing to control it.'
}

Add-Type @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
public static class DesktopPortabilityWindows {
    private delegate bool EnumWindow(IntPtr window, IntPtr parameter);
    [DllImport("user32.dll")] private static extern bool EnumWindows(EnumWindow callback, IntPtr parameter);
    [DllImport("user32.dll")] private static extern bool IsWindowVisible(IntPtr window);
    [DllImport("user32.dll")] private static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] private static extern uint GetWindowThreadProcessId(IntPtr window, out uint processId);
    [DllImport("user32.dll", EntryPoint = "SendMessageTimeoutW", SetLastError = true)] private static extern IntPtr SendMessageTimeout(
        IntPtr window, uint message, UIntPtr wParam, IntPtr lParam,
        uint flags, uint milliseconds, out UIntPtr result);
    public static IntPtr[] Visible(uint processId) {
        var windows = new List<IntPtr>();
        EnumWindows((window, parameter) => {
            uint owner;
            GetWindowThreadProcessId(window, out owner);
            if (owner == processId && IsWindowVisible(window)) windows.Add(window);
            return true;
        }, IntPtr.Zero);
        return windows.ToArray();
    }
    public static bool Responsive(IntPtr window) {
        UIntPtr result;
        return SendMessageTimeout(window, 0, UIntPtr.Zero, IntPtr.Zero, 2, 2000, out result) != IntPtr.Zero;
    }
    public static uint ForegroundProcess() {
        uint owner;
        GetWindowThreadProcessId(GetForegroundWindow(), out owner);
        return owner;
    }
}
'@

function Save-FailureScreenshot {
    try {
        Add-Type -AssemblyName System.Drawing
        Add-Type -AssemblyName System.Windows.Forms
        $bounds = [Windows.Forms.SystemInformation]::VirtualScreen
        $bitmap = [Drawing.Bitmap]::new($bounds.Width, $bounds.Height)
        $graphics = [Drawing.Graphics]::FromImage($bitmap)
        try {
            $graphics.CopyFromScreen($bounds.Left, $bounds.Top, 0, 0, $bitmap.Size)
            $bitmap.Save((Join-Path $output 'failure.png'), [Drawing.Imaging.ImageFormat]::Png)
        } finally {
            $graphics.Dispose()
            $bitmap.Dispose()
        }
    } catch {
        $_ | Out-String | Set-Content -LiteralPath (Join-Path $output 'screenshot.log')
    }
}

function Wait-Windows([Diagnostics.Process]$Process, [int]$Expected) {
    $deadline = [DateTime]::UtcNow.AddSeconds(25)
    do {
        $Process.Refresh()
        if ($Process.HasExited) { throw "Desktop exited before $Expected windows appeared." }
        $windows = @([DesktopPortabilityWindows]::Visible($Process.Id))
        if ($windows.Count -ge $Expected) { return $windows }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "Expected $Expected top-level windows; found $($windows.Count)."
}

$previousState = [Environment]::GetEnvironmentVariable('RHIZOME_DESKTOP_DATA_DIR', 'Process')
$env:RHIZOME_DESKTOP_DATA_DIR = Join-Path $output ('state-' + [Guid]::NewGuid().ToString('N'))
$process = $null
try {
    $process = Start-Process -FilePath $app -PassThru `
        -RedirectStandardOutput (Join-Path $output 'stdout.log') `
        -RedirectStandardError (Join-Path $output 'stderr.log')
    $windows = @(Wait-Windows $process 1)
    if ($windows.Count -ne 1) { throw "Fresh desktop state opened $($windows.Count) windows instead of one." }
    if (-not [DesktopPortabilityWindows]::Responsive($windows[0])) { throw 'Initial desktop window is unresponsive.' }
    $shell = New-Object -ComObject WScript.Shell
    if (-not $shell.AppActivate($process.Id)) { throw 'Could not focus the desktop app.' }
    $focusDeadline = [DateTime]::UtcNow.AddSeconds(5)
    while ([DesktopPortabilityWindows]::ForegroundProcess() -ne $process.Id) {
        if ([DateTime]::UtcNow -ge $focusDeadline) { throw 'Desktop did not become the foreground app.' }
        Start-Sleep -Milliseconds 100
    }
    $shell.SendKeys('^n')
    $windows = @(Wait-Windows $process 2)
    foreach ($window in $windows) {
        if (-not [DesktopPortabilityWindows]::Responsive($window)) { throw 'Desktop stopped responding after New Window.' }
    }
    @{
        platform = 'windows'
        initialWindows = 1
        newWindowCount = $windows.Count
        pid = $process.Id
    } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $output 'smoke.json')
    Write-Host 'Native desktop startup and New Window passed.'
} catch {
    Save-FailureScreenshot
    throw
} finally {
    [Environment]::SetEnvironmentVariable('RHIZOME_DESKTOP_DATA_DIR', $previousState, 'Process')
    if ($null -ne $process) {
        $process.Refresh()
        if (-not $process.HasExited) {
            Stop-Process -Id $process.Id
            $null = $process.WaitForExit(5000)
        }
        $process.Dispose()
    }
}
