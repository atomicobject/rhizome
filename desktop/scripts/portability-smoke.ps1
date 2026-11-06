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
    [DllImport("user32.dll")] private static extern IntPtr GetMenu(IntPtr window);
    [DllImport("user32.dll")] private static extern IntPtr GetSubMenu(IntPtr menu, int index);
    [DllImport("user32.dll")] private static extern int GetMenuItemCount(IntPtr menu);
    [DllImport("user32.dll")] private static extern uint GetMenuItemID(IntPtr menu, int index);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] private static extern int GetMenuString(IntPtr menu, uint index, StringBuilder text, int count, uint flags);
    [DllImport("user32.dll", EntryPoint = "PostMessageW")] public static extern bool PostMessage(IntPtr window, uint message, UIntPtr wParam, IntPtr lParam);
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
    public static object NewWindowMenu(IntPtr window) {
        var menu = GetMenu(window);
        for (var section = 0; section < GetMenuItemCount(menu); section++) {
            var submenu = GetSubMenu(menu, section);
            for (var index = 0; index < GetMenuItemCount(submenu); index++) {
                var text = new StringBuilder(512);
                GetMenuString(submenu, (uint)index, text, text.Capacity, 0x400);
                var caption = text.ToString();
                if (caption.Split('\t')[0].Replace("&", "") == "New Window") {
                    return new { id = GetMenuItemID(submenu, index), caption = caption };
                }
            }
        }
        return null;
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

function Test-IndependentInput($Shell) {
    $directory = Join-Path $output ('input-calibration-' + [Guid]::NewGuid().ToString('N'))
    $null = New-Item -ItemType Directory -Path $directory
    $script = Join-Path $directory 'control.ps1'
    @'
param([string]$EvidenceDir)
$ErrorActionPreference = 'Stop'
try {
Add-Type -AssemblyName System.Windows.Forms
$form = [Windows.Forms.Form]::new()
$form.Text = 'Rhizome input calibration'
$form.KeyPreview = $true
$textBox = [Windows.Forms.TextBox]::new()
$textBox.Dock = 'Fill'
$textBox.Add_TextChanged({
    param($sender, $change)
    if ($sender.Text -eq 'rhizomeinput') {
        [IO.File]::WriteAllText((Join-Path $EvidenceDir 'text.txt'), $sender.Text)
    }
})
$form.Add_KeyDown({
    param($sender, $key)
    if ($key.Control -and $key.KeyCode -eq [Windows.Forms.Keys]::N) {
        [IO.File]::WriteAllText((Join-Path $EvidenceDir 'chord.txt'), 'Ctrl+N')
        $key.SuppressKeyPress = $true
    }
})
$form.Controls.Add($textBox)
$form.Add_Shown({
    try {
        if (-not $textBox.Focus()) { throw 'Calibration textbox could not receive focus.' }
        [IO.File]::WriteAllText((Join-Path $EvidenceDir 'ready.tmp'), $textBox.Handle.ToInt64().ToString())
        [IO.File]::Move((Join-Path $EvidenceDir 'ready.tmp'), (Join-Path $EvidenceDir 'ready.txt'))
    } catch {
        [IO.File]::WriteAllText((Join-Path $EvidenceDir 'control-error.txt'), $_.Exception.Message)
        $form.Close()
    }
})
$null = $form.ShowDialog()
} catch {
    [IO.File]::WriteAllText((Join-Path $EvidenceDir 'control-error.txt'), $_.Exception.Message)
    exit 1
}
'@ | Set-Content -LiteralPath $script
    $start = [Diagnostics.ProcessStartInfo]::new([Diagnostics.Process]::GetCurrentProcess().MainModule.FileName)
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    foreach ($argument in @('-NoProfile', '-STA', '-File', $script, '-EvidenceDir', $directory)) {
        $start.ArgumentList.Add($argument)
    }
    $control = [Diagnostics.Process]::Start($start)
    $evidence = @{ pid = $control.Id; evidenceDirectory = $directory; ready = $false; inputAttempted = $false }
    try {
        $deadline = [DateTime]::UtcNow.AddSeconds(5)
        do {
            $errorFile = Join-Path $directory 'control-error.txt'
            if (Test-Path -LiteralPath $errorFile) { throw [IO.File]::ReadAllText($errorFile) }
            $control.Refresh()
            if ($control.HasExited) { throw 'Independent native input control exited before readiness.' }
            $windows = @([DesktopPortabilityWindows]::Visible($control.Id) | Where-Object {
                -not [DesktopPortabilityWindows]::ToolWindow($_)
            })
            $ready = Test-Path -LiteralPath (Join-Path $directory 'ready.txt')
            if ($windows.Count -eq 1 -and $ready) { break }
            Start-Sleep -Milliseconds 100
        } while ([DateTime]::UtcNow -lt $deadline)
        if ($windows.Count -ne 1) { throw 'Independent native input control did not open.' }
        if (-not $ready) { throw 'Independent native input control did not report textbox readiness.' }
        $evidence.ready = $true
        $evidence.textBoxHandle = [IO.File]::ReadAllText((Join-Path $directory 'ready.txt'))
        $target = $windows[0]
        $evidence.controlWindow = [DesktopPortabilityWindows]::Describe($target)
        $null = $Shell.AppActivate($control.Id)
        $null = [DesktopPortabilityWindows]::SetForegroundWindow($target)
        while ([DesktopPortabilityWindows]::GetForegroundWindow() -ne $target -and [DateTime]::UtcNow -lt $deadline) {
            Start-Sleep -Milliseconds 100
        }
        $evidence.foregroundBeforeInput = [DesktopPortabilityWindows]::Describe([DesktopPortabilityWindows]::GetForegroundWindow())
        if ([DesktopPortabilityWindows]::GetForegroundWindow() -ne $target) {
            throw 'Independent native input control did not become foreground.'
        }
        $evidence.inputAttempted = $true
        $Shell.SendKeys('rhizomeinput')
        $Shell.SendKeys('^n')
        $deadline = [DateTime]::UtcNow.AddSeconds(2)
        do {
            $text = Test-Path -LiteralPath (Join-Path $directory 'text.txt')
            $chord = Test-Path -LiteralPath (Join-Path $directory 'chord.txt')
            if ($text -and $chord) { break }
            Start-Sleep -Milliseconds 100
        } while ([DateTime]::UtcNow -lt $deadline)
        $evidence.textReceived = $text
        $evidence.ctrlNReceived = $chord
        $evidence.foregroundAfterInput = [DesktopPortabilityWindows]::Describe([DesktopPortabilityWindows]::GetForegroundWindow())
    } catch {
        $evidence.error = $_.Exception.Message
    } finally {
        $control.Refresh()
        if (-not $control.HasExited) {
            $control.Kill()
            $null = $control.WaitForExit(5000)
        }
        $control.Dispose()
    }
    return $evidence
}

function Save-ShortcutDiagnostic([Diagnostics.Process]$Process, [IntPtr]$Window, $Shell) {
    $diagnostic = @{ applicationResponsive = [DesktopPortabilityWindows]::Responsive($Window) }
    try { $diagnostic.inputControl = Test-IndependentInput $Shell }
    catch { $diagnostic.inputControl = @{ error = $_.Exception.Message } }
    $menu = [DesktopPortabilityWindows]::NewWindowMenu($Window)
    $diagnostic.menu = $menu
    if ($null -ne $menu) {
        Save-WindowEvidence $Process 'before-menu-diagnostic'
        $beforeMenu = @([DesktopPortabilityWindows]::Visible($Process.Id) | Where-Object {
            -not [DesktopPortabilityWindows]::ToolWindow($_)
        })
        $diagnostic.applicationWindowsBeforeMenu = $beforeMenu.Count
        $diagnostic.additionalWindowBeforeMenu = $beforeMenu.Count -gt 1
        $diagnostic.menuCommandQueued = [DesktopPortabilityWindows]::PostMessage($Window, 0x111, [UIntPtr]$menu.id, [IntPtr]::Zero)
        $deadline = [DateTime]::UtcNow.AddSeconds(5)
        do {
            $windows = @([DesktopPortabilityWindows]::Visible($Process.Id) | Where-Object {
                -not [DesktopPortabilityWindows]::ToolWindow($_)
            })
            if ($windows.Count -ge $beforeMenu.Count + 1) { break }
            Start-Sleep -Milliseconds 100
        } while ([DateTime]::UtcNow -lt $deadline)
        $diagnostic.menuApplicationWindows = $windows.Count
        $diagnostic.menuWindowDelta = $windows.Count - $beforeMenu.Count
        $diagnostic.menuWindowIncreaseObserved = $windows.Count -ge $beforeMenu.Count + 1
        $diagnostic.applicationResponsiveAfterMenu = [DesktopPortabilityWindows]::Responsive($Window)
    }
    Save-WindowEvidence $Process 'menu-diagnostic'
    $diagnostic | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $output 'shortcut-diagnostic.json')
}

$previousState = [Environment]::GetEnvironmentVariable('RHIZOME_DESKTOP_DATA_DIR', 'Process')
$env:RHIZOME_DESKTOP_DATA_DIR = Join-Path $output ('state-' + [Guid]::NewGuid().ToString('N'))
$process = $null
$keyboardAttempted = $false
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
    $keyboardAttempted = $true
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
    $failure = $_
    if ($null -ne $process) {
        try { Save-WindowEvidence $process 'failure' }
        catch { $_ | Out-String | Set-Content -LiteralPath (Join-Path $output 'window-evidence.log') }
    }
    Save-FailureScreenshot
    # Diagnostics cannot turn the original keyboard failure into a passing smoke check.
    if ($keyboardAttempted) {
        try { Save-ShortcutDiagnostic $process $targetWindow $shell }
        catch { $_ | Out-String | Set-Content -LiteralPath (Join-Path $output 'shortcut-diagnostic.log') }
    }
    throw $failure
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
