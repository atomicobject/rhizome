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

Add-Type -Path (Join-Path $PSScriptRoot 'portability-windows.cs')
[DesktopPortabilityWindows]::ValidateInterop()

function Save-WindowEvidence([Diagnostics.Process]$Process, [string]$Stage, [IntPtr]$ApplicationWindow = [IntPtr]::Zero) {
    if ($ApplicationWindow -eq [IntPtr]::Zero) {
        $applications = @([DesktopPortabilityWindows]::ApplicationWindows($Process.Id))
        if ($applications.Count -gt 0) { $ApplicationWindow = $applications[0] }
    }
    @{
        stage = $Stage
        utc = [DateTime]::UtcNow.ToString('O')
        pid = $Process.Id
        foregroundWindow = [DesktopPortabilityWindows]::Describe([DesktopPortabilityWindows]::GetForegroundWindow())
        focus = [DesktopPortabilityWindows]::FocusEvidence($ApplicationWindow)
        newWindowMenu = [DesktopPortabilityWindows]::NewWindowMenu($ApplicationWindow)
        visibleWindows = @([DesktopPortabilityWindows]::Visible($Process.Id) | ForEach-Object {
            [DesktopPortabilityWindows]::Describe($_)
        })
    } | ConvertTo-Json -Depth 7 | Set-Content -LiteralPath (Join-Path $output "$Stage-windows.json")
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

function Wait-VisibleChildFocus([Diagnostics.Process]$Process, [IntPtr]$Window) {
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    do {
        $Process.Refresh()
        if ($Process.HasExited) { throw 'Desktop exited before the visible child received focus.' }
        $focused = [DesktopPortabilityWindows]::VisibleFocusedChild($Window)
        if ($focused -ne [IntPtr]::Zero) { return $focused }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'The new desktop window did not focus a visible child after the shell click.'
}

$previousState = [Environment]::GetEnvironmentVariable('RHIZOME_DESKTOP_DATA_DIR', 'Process')
$env:RHIZOME_DESKTOP_DATA_DIR = Join-Path $output ('state-' + [Guid]::NewGuid().ToString('N'))
$process = $null
$targetWindow = [IntPtr]::Zero
try {
    $process = Start-Process -FilePath $app -PassThru `
        -RedirectStandardOutput (Join-Path $output 'stdout.log') `
        -RedirectStandardError (Join-Path $output 'stderr.log')
    $windows = @(Wait-Windows $process 1)
    Save-WindowEvidence $process 'initial'
    if ($windows.Count -ne 1) { throw "Fresh desktop state opened $($windows.Count) windows instead of one." }
    $initialWindows = $windows.Count
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
    Save-WindowEvidence $process 'focused' $targetWindow
    $shell.SendKeys('^n')
    $windows = @(Wait-Windows $process 2)
    Save-WindowEvidence $process 'new-window' $targetWindow
    if ($windows.Count -ne 2) { throw "New Window opened $($windows.Count) application windows instead of two." }
    $firstKeyWindows = $windows.Count
    foreach ($window in $windows) {
        if (-not [DesktopPortabilityWindows]::Responsive($window)) { throw 'Desktop stopped responding after New Window.' }
    }

    $newWindows = @($windows | Where-Object { $_ -ne $targetWindow })
    if ($newWindows.Count -ne 1) { throw 'Could not identify the newly created desktop window.' }
    $targetWindow = $newWindows[0]
    $null = [DesktopPortabilityWindows]::SetForegroundWindow($targetWindow)
    $focusDeadline = [DateTime]::UtcNow.AddSeconds(5)
    while ([DesktopPortabilityWindows]::GetForegroundWindow() -ne $targetWindow) {
        if ([DateTime]::UtcNow -ge $focusDeadline) { throw 'The new desktop window did not become foreground.' }
        Start-Sleep -Milliseconds 100
    }
    Save-WindowEvidence $process 'visible-child-before-click' $targetWindow
    $click = [DesktopPortabilityWindows]::ClickBlankShell($targetWindow)
    $click | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $output 'visible-child-click.json')
    if (-not $click.eligible -or $click.insertedEvents -ne 2) { throw 'Could not click the new window visible shell.' }
    $focusedChild = Wait-VisibleChildFocus $process $targetWindow
    Save-WindowEvidence $process 'visible-child-focused' $targetWindow
    $beforeChildKey = @([DesktopPortabilityWindows]::ApplicationWindows($process.Id))
    if ($beforeChildKey.Count -ne 2) { throw 'Desktop window count changed before the visible-child shortcut.' }
    $shell.SendKeys('^n')
    $windows = @(Wait-Windows $process 3)
    Save-WindowEvidence $process 'visible-child-new-window' $targetWindow
    if ($windows.Count -ne 3) { throw "The visible-child shortcut opened $($windows.Count) application windows instead of three." }
    foreach ($window in $windows) {
        if (-not [DesktopPortabilityWindows]::Responsive($window)) { throw 'Desktop stopped responding after the visible-child shortcut.' }
    }
    @{
        platform = 'windows'
        firstKeyBeforeWindows = $initialWindows
        firstKeyAfterWindows = $firstKeyWindows
        visibleChildKeyBeforeWindows = $beforeChildKey.Count
        visibleChildKeyAfterWindows = $windows.Count
        visibleChildHandle = [DesktopPortabilityWindows]::Describe($focusedChild).handle
        pid = $process.Id
    } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $output 'smoke.json')
    Write-Host 'Native desktop startup and both New Window keyboard paths passed.'
} catch {
    $failure = $_
    if ($null -ne $process) {
        try { Save-WindowEvidence $process 'failure' $targetWindow }
        catch { $_ | Out-String | Set-Content -LiteralPath (Join-Path $output 'window-evidence.log') }
    }
    Save-FailureScreenshot
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
