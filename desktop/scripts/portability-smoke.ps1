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
using System.Text;
public static class DesktopPortabilityWindows {
    private delegate bool EnumWindow(IntPtr window, IntPtr parameter);
    [StructLayout(LayoutKind.Sequential)] private struct Rect { public int Left, Top, Right, Bottom; }
    [DllImport("user32.dll")] private static extern bool EnumWindows(EnumWindow callback, IntPtr parameter);
    [DllImport("user32.dll")] private static extern bool IsWindowVisible(IntPtr window);
    [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr window);
    [DllImport("user32.dll")] private static extern uint GetWindowThreadProcessId(IntPtr window, out uint processId);
    [DllImport("user32.dll", EntryPoint = "GetWindowLongW")] private static extern int GetWindowLong(IntPtr window, int index);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] private static extern int GetClassName(IntPtr window, StringBuilder name, int count);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] private static extern int GetWindowText(IntPtr window, StringBuilder text, int count);
    [DllImport("user32.dll")] private static extern bool GetWindowRect(IntPtr window, out Rect bounds);
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
    public static bool ToolWindow(IntPtr window) {
        // The single-instance plugin uses a visible layered tool window for IPC.
        return (GetWindowLong(window, -20) & 0x80) != 0;
    }
    public static object Describe(IntPtr window) {
        if (window == IntPtr.Zero) return null;
        var name = new StringBuilder(256);
        var title = new StringBuilder(512);
        GetClassName(window, name, name.Capacity);
        GetWindowText(window, title, title.Capacity);
        Rect bounds;
        GetWindowRect(window, out bounds);
        return new {
            handle = "0x" + window.ToInt64().ToString("X"),
            className = name.ToString(),
            title = title.ToString(),
            extendedStyle = "0x" + unchecked((uint)GetWindowLong(window, -20)).ToString("X8"),
            width = bounds.Right - bounds.Left,
            height = bounds.Bottom - bounds.Top,
            applicationWindow = !ToolWindow(window)
        };
    }
    public static bool Responsive(IntPtr window) {
        UIntPtr result;
        return SendMessageTimeout(window, 0, UIntPtr.Zero, IntPtr.Zero, 2, 2000, out result) != IntPtr.Zero;
    }
}
'@

function Save-WindowEvidence([Diagnostics.Process]$Process, [string]$Stage) {
    @{
        stage = $Stage
        pid = $Process.Id
        foregroundWindow = [DesktopPortabilityWindows]::Describe([DesktopPortabilityWindows]::GetForegroundWindow())
        visibleWindows = @([DesktopPortabilityWindows]::Visible($Process.Id) | ForEach-Object {
            [DesktopPortabilityWindows]::Describe($_)
        })
    } | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $output "$Stage-windows.json")
}

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
        $windows = @([DesktopPortabilityWindows]::Visible($Process.Id) | Where-Object {
            -not [DesktopPortabilityWindows]::ToolWindow($_)
        })
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
    Save-WindowEvidence $process 'initial'
    if ($windows.Count -ne 1) { throw "Fresh desktop state opened $($windows.Count) windows instead of one." }
    if (-not [DesktopPortabilityWindows]::Responsive($windows[0])) { throw 'Initial desktop window is unresponsive.' }
    $shell = New-Object -ComObject WScript.Shell
    if (-not $shell.AppActivate($process.Id)) { throw 'Could not focus the desktop app.' }
    # A process also owns IPC tool windows; target the classified application HWND.
    $targetWindow = $windows[0]
    $null = [DesktopPortabilityWindows]::SetForegroundWindow($targetWindow)
    $focusDeadline = [DateTime]::UtcNow.AddSeconds(5)
    while ([DesktopPortabilityWindows]::GetForegroundWindow() -ne $targetWindow) {
        if ([DateTime]::UtcNow -ge $focusDeadline) { throw 'The desktop application window did not become foreground.' }
        Start-Sleep -Milliseconds 100
    }
    Save-WindowEvidence $process 'focused'
    $shell.SendKeys('^n')
    $windows = @(Wait-Windows $process 2)
    Save-WindowEvidence $process 'new-window'
    if ($windows.Count -ne 2) { throw "New Window opened $($windows.Count) application windows instead of two." }
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
    if ($null -ne $process) {
        try { Save-WindowEvidence $process 'failure' }
        catch { $_ | Out-String | Set-Content -LiteralPath (Join-Path $output 'window-evidence.log') }
    }
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
