param(
    [Parameter(Mandatory = $true)][string]$AppPath,
    [Parameter(Mandatory = $true)][string]$OutputDir,
    [switch]$TraceKeys
)

$ErrorActionPreference = 'Stop'
$app = (Resolve-Path -LiteralPath $AppPath).ProviderPath
$output = [IO.Path]::GetFullPath($OutputDir)
$null = New-Item -ItemType Directory -Force -Path $output
$caseInfo = @{
    caseId = [IO.DirectoryInfo]::new($output).Name
    traceEnabled = [bool]$TraceKeys
    startedUtc = [DateTime]::UtcNow.ToString('O')
}
$caseInfo | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $output 'case.json')
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

function Write-KeyPhase([string]$Phase) {
    $utc = [DateTime]::UtcNow.ToString('O')
    "$utc $Phase" | Add-Content -LiteralPath (Join-Path $output 'keyboard-phases.log')
}

function Save-SendInputDiagnostic([Diagnostics.Process]$Process, [IntPtr]$Window, [IntPtr]$FocusedChild) {
    $record = @{ stage = 'after-primary-failure'; utc = [DateTime]::UtcNow.ToString('O') }
    try {
        $Process.Refresh()
        if ($Process.HasExited) { $record.outcome = 'skipped-app-exited'; return }
        $before = @([DesktopPortabilityWindows]::ApplicationWindows($Process.Id))
        $record.before = @{
            utc = [DateTime]::UtcNow.ToString('O')
            count = $before.Count
            focus = [DesktopPortabilityWindows]::FocusEvidence($Window)
            windows = @($before | ForEach-Object { [DesktopPortabilityWindows]::Describe($_) })
        }
        if ($before.Count -ne 2) {
            $record.outcome = if ($before.Count -gt 2) { 'late-window-before-probe' } else { 'skipped-window-count-changed' }
            return
        }
        Save-WindowEvidence $Process 'send-input-before' $Window
        Write-KeyPhase 'send-input-after-failure'
        $record.input = [DesktopPortabilityWindows]::SendCtrlNDiagnostic($Window, $FocusedChild, $Process.Id)
        if (-not $record.input.eligible) {
            $record.outcome = if ($record.input.applicationWindowsBeforeInput -gt 2) { 'late-window-before-probe' } else { 'skipped-input-precondition' }
            return
        }
        $deadline = [DateTime]::UtcNow.AddSeconds(5)
        do {
            $Process.Refresh()
            $after = @([DesktopPortabilityWindows]::ApplicationWindows($Process.Id))
            if ($Process.HasExited -or $after.Count -ne $before.Count) { break }
            Start-Sleep -Milliseconds 100
        } while ([DateTime]::UtcNow -lt $deadline)
        $record.after = @{
            utc = [DateTime]::UtcNow.ToString('O')
            exited = $Process.HasExited
            count = $after.Count
            delta = $after.Count - $before.Count
            focus = [DesktopPortabilityWindows]::FocusEvidence($Window)
            windows = @($after | ForEach-Object { [DesktopPortabilityWindows]::Describe($_) })
        }
        $record.outcome = if ($record.input.insertedEvents -ne 4) { 'partial-input-insertion' } else { 'probe-complete' }
        Save-WindowEvidence $Process 'send-input-after' $Window
    } catch {
        $record.error = $_ | Out-String
    } finally {
        $record | ConvertTo-Json -Depth 9 | Set-Content -LiteralPath (Join-Path $output 'send-input-diagnostic.json')
    }
}

$previousState = [Environment]::GetEnvironmentVariable('RHIZOME_DESKTOP_DATA_DIR', 'Process')
$previousKeyTrace = [Environment]::GetEnvironmentVariable('RZM_DESKTOP_KEY_TRACE', 'Process')
$env:RHIZOME_DESKTOP_DATA_DIR = Join-Path $output ('state-' + [Guid]::NewGuid().ToString('N'))
$process = $null
$targetWindow = [IntPtr]::Zero
$visibleChildKeyAttempted = $false
try {
    $traceValue = if ($TraceKeys) { 'ctrl-n' } else { $null }
    [Environment]::SetEnvironmentVariable('RZM_DESKTOP_KEY_TRACE', $traceValue, 'Process')
    $process = Start-Process -FilePath $app -PassThru `
        -RedirectStandardOutput (Join-Path $output 'stdout.log') `
        -RedirectStandardError (Join-Path $output 'stderr.log')
    [Environment]::SetEnvironmentVariable('RZM_DESKTOP_KEY_TRACE', $previousKeyTrace, 'Process')
    $caseInfo.pid = $process.Id
    $caseInfo | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $output 'case.json')
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
    Write-KeyPhase 'wsh-first-ctrl-n-send'
    $shell.SendKeys('^n')
    Write-KeyPhase 'wsh-first-ctrl-n-returned'
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
    Write-KeyPhase 'wsh-visible-child-ctrl-n-send'
    $visibleChildKeyAttempted = $true
    $shell.SendKeys('^n')
    Write-KeyPhase 'wsh-visible-child-ctrl-n-returned'
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
    if ($visibleChildKeyAttempted -and $null -ne $process) {
        try { Save-SendInputDiagnostic $process $targetWindow $focusedChild }
        catch {
            try { $_ | Out-String | Set-Content -LiteralPath (Join-Path $output 'send-input-diagnostic.log') }
            catch { }
        }
    }
    throw $failure
} finally {
    [Environment]::SetEnvironmentVariable('RHIZOME_DESKTOP_DATA_DIR', $previousState, 'Process')
    [Environment]::SetEnvironmentVariable('RZM_DESKTOP_KEY_TRACE', $previousKeyTrace, 'Process')
    if ($null -ne $process) {
        $process.Refresh()
        if (-not $process.HasExited) {
            Stop-Process -Id $process.Id
            $null = $process.WaitForExit(5000)
        }
        $process.Dispose()
    }
}
