using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;
public static class DesktopPortabilityWindows {
    private delegate bool EnumWindow(IntPtr window, IntPtr parameter);
    [StructLayout(LayoutKind.Sequential)] private struct Rect { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] private struct Point { public int X, Y; }
    [StructLayout(LayoutKind.Sequential)] private struct GuiThreadInfo {
        public uint Size, Flags;
        public IntPtr Active, Focus, Capture, MenuOwner, MoveSize, Caret;
        public Rect CaretBounds;
    }
    [StructLayout(LayoutKind.Sequential)] private struct MouseInput {
        public int X, Y;
        public uint MouseData, Flags, Time;
        public UIntPtr ExtraInfo;
    }
    [StructLayout(LayoutKind.Sequential)] private struct Input {
        public uint Type;
        public MouseInput Mouse;
    }
    [DllImport("user32.dll")] private static extern bool EnumWindows(EnumWindow callback, IntPtr parameter);
    [DllImport("user32.dll")] private static extern bool IsWindowVisible(IntPtr window);
    [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr window);
    [DllImport("user32.dll")] private static extern uint GetWindowThreadProcessId(IntPtr window, out uint processId);
    [DllImport("user32.dll", EntryPoint = "GetWindowLongW")] private static extern int GetWindowLong(IntPtr window, int index);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] private static extern int GetClassName(IntPtr window, StringBuilder name, int count);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] private static extern int GetWindowText(IntPtr window, StringBuilder text, int count);
    [DllImport("user32.dll")] private static extern bool GetWindowRect(IntPtr window, out Rect bounds);
    [DllImport("user32.dll")] private static extern bool IsWindowEnabled(IntPtr window);
    [DllImport("user32.dll")] private static extern IntPtr GetParent(IntPtr window);
    [DllImport("user32.dll")] private static extern IntPtr GetAncestor(IntPtr window, uint flags);
    [DllImport("user32.dll")] private static extern bool IsChild(IntPtr parent, IntPtr window);
    [DllImport("user32.dll", SetLastError = true)] private static extern bool GetGUIThreadInfo(uint thread, ref GuiThreadInfo info);
    [DllImport("user32.dll", SetLastError = true)] private static extern bool GetClientRect(IntPtr window, out Rect bounds);
    [DllImport("user32.dll", SetLastError = true)] private static extern bool ClientToScreen(IntPtr window, ref Point point);
    [DllImport("user32.dll")] private static extern IntPtr WindowFromPoint(Point point);
    [DllImport("user32.dll", SetLastError = true)] private static extern bool SetCursorPos(int x, int y);
    [DllImport("user32.dll", SetLastError = true)] private static extern uint SendInput(uint count, Input[] inputs, int size);
    [DllImport("user32.dll")] private static extern IntPtr GetMenu(IntPtr window);
    [DllImport("user32.dll")] private static extern IntPtr GetSubMenu(IntPtr menu, int index);
    [DllImport("user32.dll")] private static extern int GetMenuItemCount(IntPtr menu);
    [DllImport("user32.dll")] private static extern uint GetMenuItemID(IntPtr menu, int index);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] private static extern int GetMenuString(IntPtr menu, uint index, StringBuilder text, int count, uint flags);
    [DllImport("user32.dll", EntryPoint = "SendMessageTimeoutW", SetLastError = true)] private static extern IntPtr SendMessageTimeout(
        IntPtr window, uint message, UIntPtr wParam, IntPtr lParam,
        uint flags, uint milliseconds, out UIntPtr result);
    public static void ValidateInterop() {
        if (IntPtr.Size != 8 || Marshal.SizeOf(typeof(GuiThreadInfo)) != 72 ||
            Marshal.OffsetOf(typeof(GuiThreadInfo), "Active").ToInt64() != 8 ||
            Marshal.OffsetOf(typeof(GuiThreadInfo), "Focus").ToInt64() != 16 ||
            Marshal.OffsetOf(typeof(GuiThreadInfo), "CaretBounds").ToInt64() != 56)
            throw new InvalidOperationException("The Windows x64 GUITHREADINFO layout is incorrect.");
        if (Marshal.SizeOf(typeof(Input)) != 40 || Marshal.SizeOf(typeof(MouseInput)) != 32 ||
            Marshal.OffsetOf(typeof(Input), "Mouse").ToInt64() != 8 ||
            Marshal.OffsetOf(typeof(MouseInput), "ExtraInfo").ToInt64() != 24)
            throw new InvalidOperationException("The Windows x64 SendInput layout is incorrect.");
    }
    public static IntPtr[] Owned(uint processId) {
        var windows = new List<IntPtr>();
        EnumWindows((window, parameter) => {
            uint owner;
            GetWindowThreadProcessId(window, out owner);
            if (owner == processId) windows.Add(window);
            return true;
        }, IntPtr.Zero);
        return windows.ToArray();
    }
    public static IntPtr[] Visible(uint processId) {
        var windows = new List<IntPtr>();
        foreach (var window in Owned(processId)) {
            if (IsWindowVisible(window)) windows.Add(window);
        }
        return windows.ToArray();
    }
    public static bool ToolWindow(IntPtr window) {
        // The single-instance plugin uses a visible layered tool window for IPC.
        return (GetWindowLong(window, -20) & 0x80) != 0;
    }
    public static IntPtr[] ApplicationWindows(uint processId) {
        var windows = new List<IntPtr>();
        foreach (var window in Visible(processId)) {
            if (!ToolWindow(window)) windows.Add(window);
        }
        return windows.ToArray();
    }
    public static object Describe(IntPtr window) {
        if (window == IntPtr.Zero) return null;
        var name = new StringBuilder(256);
        var title = new StringBuilder(512);
        GetClassName(window, name, name.Capacity);
        GetWindowText(window, title, title.Capacity);
        Rect bounds;
        GetWindowRect(window, out bounds);
        uint process;
        var thread = GetWindowThreadProcessId(window, out process);
        return new {
            handle = "0x" + window.ToInt64().ToString("X"),
            className = name.ToString(),
            processId = process,
            threadId = thread,
            visible = IsWindowVisible(window),
            parentHandle = "0x" + GetParent(window).ToInt64().ToString("X"),
            rootHandle = "0x" + GetAncestor(window, 2).ToInt64().ToString("X"),
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
    private static bool ReadThreadInfo(uint thread, out GuiThreadInfo info) {
        info = new GuiThreadInfo { Size = (uint)Marshal.SizeOf(typeof(GuiThreadInfo)) };
        return GetGUIThreadInfo(thread, ref info);
    }
    public static object GuiThread(uint thread) {
        GuiThreadInfo info;
        var available = ReadThreadInfo(thread, out info);
        var error = available ? 0 : Marshal.GetLastWin32Error();
        return new {
            requestedThreadId = thread, available = available, error = error, flags = info.Flags,
            active = available ? Describe(info.Active) : null,
            focus = available ? Describe(info.Focus) : null,
            caret = available ? Describe(info.Caret) : null,
            capture = available ? Describe(info.Capture) : null,
            menuOwner = available ? Describe(info.MenuOwner) : null
        };
    }
    public static object FocusEvidence(IntPtr window) {
        uint process;
        var thread = window == IntPtr.Zero ? 0 : GetWindowThreadProcessId(window, out process);
        return new {
            foreground = Describe(GetForegroundWindow()),
            foregroundThread = GuiThread(0),
            applicationThread = thread == 0 ? null : GuiThread(thread)
        };
    }
    public static object ClickBlankShell(IntPtr window) {
        if (GetForegroundWindow() != window)
            return new { eligible = false, error = "The original application is not foreground." };
        Rect client;
        if (!GetClientRect(window, out client))
            return new { eligible = false, error = "Cannot read the application client bounds." };
        var point = new Point { X = (client.Right - client.Left) * 3 / 4, Y = (client.Bottom - client.Top) * 3 / 4 };
        if (!ClientToScreen(window, ref point))
            return new { eligible = false, error = "Cannot locate the blank client area." };
        var target = WindowFromPoint(point);
        if (target == IntPtr.Zero || target == window || !IsChild(window, target) ||
            !IsWindowVisible(target) || !IsWindowEnabled(target))
            return new { eligible = false, error = "The blank-shell point has no visible owned child.", target = Describe(target) };
        if (!SetCursorPos(point.X, point.Y))
            return new { eligible = false, error = "Cannot position the diagnostic pointer." };
        var inputs = new[] {
            new Input { Type = 0, Mouse = new MouseInput { Flags = 0x0002 } },
            new Input { Type = 0, Mouse = new MouseInput { Flags = 0x0004 } }
        };
        var inserted = SendInput((uint)inputs.Length, inputs, Marshal.SizeOf(typeof(Input)));
        var error = inserted == inputs.Length ? 0 : Marshal.GetLastWin32Error();
        return new {
            eligible = true, x = point.X, y = point.Y, target = Describe(target),
            insertedEvents = inserted, expectedEvents = inputs.Length, error = error
        };
    }
    public static IntPtr VisibleFocusedChild(IntPtr window) {
        if (GetForegroundWindow() != window) return IntPtr.Zero;
        GuiThreadInfo info;
        if (!ReadThreadInfo(0, out info) || info.Focus == IntPtr.Zero || info.Focus == window ||
            !IsChild(window, info.Focus) || !IsWindowVisible(info.Focus)) return IntPtr.Zero;
        return info.Focus;
    }
}
