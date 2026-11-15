using System;
using System.Collections.Concurrent;
using System.Collections.Generic;
using System.Diagnostics;
using System.Runtime.InteropServices;
using System.Threading;

// Temporary diagnostic only. https://learn.microsoft.com/windows/win32/winmsg/lowlevelkeyboardproc
public sealed class DesktopKeyboardObserver {
    private const int Capacity = 256;
    private const uint ReadyMessage = 0x8001;
    private delegate IntPtr HookProc(int code, UIntPtr message, IntPtr data);
    [StructLayout(LayoutKind.Sequential)] private struct Point { public int X, Y; }
    [StructLayout(LayoutKind.Sequential)] private struct Rect { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] private struct Message {
        public IntPtr Window;
        public uint Id;
        public UIntPtr WParam;
        public IntPtr LParam;
        public uint Time;
        public Point Point;
        public uint Private;
    }
    [StructLayout(LayoutKind.Sequential)] private struct KeyboardInput {
        public uint Vk, Scan, Flags, Time;
        public UIntPtr ExtraInfo;
    }
    [StructLayout(LayoutKind.Sequential)] private struct GuiThreadInfo {
        public uint Size, Flags;
        public IntPtr Active, Focus, Capture, MenuOwner, MoveSize, Caret;
        public Rect CaretBounds;
    }
    public struct Context {
        public long Foreground, Focus, Layout;
        public uint ForegroundThread, ForegroundProcess, FocusThread, FocusProcess;
        public bool FocusAvailable;
        public int FocusError, AsyncModifiersBeforeUpdate;
    }
    public struct KeyRow {
        public long Order, UtcMs, MonotonicTicks;
        public uint Message, Vk, Scan, Flags, EventTimeMs;
        public bool Injected, LowerIntegrityInjected, Up;
        public Context Context;
    }
    [DllImport("user32.dll", SetLastError = true)] private static extern IntPtr SetWindowsHookExW(int kind, HookProc callback, IntPtr module, uint thread);
    [DllImport("user32.dll")] private static extern IntPtr CallNextHookEx(IntPtr hook, int code, UIntPtr message, IntPtr data);
    [DllImport("user32.dll", SetLastError = true)] private static extern bool UnhookWindowsHookEx(IntPtr hook);
    [DllImport("user32.dll", SetLastError = true)] private static extern int GetMessageW(out Message message, IntPtr window, uint first, uint last);
    [DllImport("user32.dll")] private static extern bool PeekMessageW(out Message message, IntPtr window, uint first, uint last, uint remove);
    [DllImport("user32.dll")] private static extern bool TranslateMessage(ref Message message);
    [DllImport("user32.dll")] private static extern IntPtr DispatchMessageW(ref Message message);
    [DllImport("user32.dll", SetLastError = true)] private static extern bool PostThreadMessageW(uint thread, uint message, UIntPtr wParam, IntPtr lParam);
    [DllImport("user32.dll", SetLastError = true)] private static extern UIntPtr SetTimer(IntPtr window, UIntPtr id, uint milliseconds, IntPtr callback);
    [DllImport("user32.dll")] private static extern bool KillTimer(IntPtr window, UIntPtr id);
    [DllImport("user32.dll")] private static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] private static extern uint GetWindowThreadProcessId(IntPtr window, out uint process);
    [DllImport("user32.dll", SetLastError = true)] private static extern bool GetGUIThreadInfo(uint thread, ref GuiThreadInfo info);
    [DllImport("user32.dll")] private static extern IntPtr GetKeyboardLayout(uint thread);
    [DllImport("user32.dll")] private static extern short GetAsyncKeyState(int key);
    [DllImport("kernel32.dll")] private static extern uint GetCurrentThreadId();
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)] private static extern IntPtr GetModuleHandleW(string name);

    private readonly KeyRow[] keys = new KeyRow[Capacity];
    private readonly List<object> markers = new List<object>();
    private readonly ConcurrentQueue<string> errors = new ConcurrentQueue<string>();
    private readonly ManualResetEvent ready = new ManualResetEvent(false);
    private readonly HookProc callback;
    private Thread thread;
    private IntPtr hook;
    private uint threadId;
    private int count;
    private long order;
    private volatile bool stopping;
    private bool overflow;
    private bool removalFailed;
    public bool Ready { get; private set; }
    public bool Healthy { get { return Ready && thread != null && !thread.IsAlive && hook == IntPtr.Zero && !overflow && errors.IsEmpty; } }

    public DesktopKeyboardObserver() { callback = ObserveKey; }

    public void Start() {
        if (IntPtr.Size != 8 || Marshal.SizeOf(typeof(KeyboardInput)) != 24 ||
            Marshal.OffsetOf(typeof(KeyboardInput), "ExtraInfo").ToInt64() != 16 ||
            Marshal.SizeOf(typeof(GuiThreadInfo)) != 72 || Marshal.SizeOf(typeof(Message)) != 48)
            throw new InvalidOperationException("Keyboard observer requires the Windows x64 native layouts.");
        thread = new Thread(Pump) { IsBackground = true, Name = "temporary-keyboard-observer" };
        thread.Start();
        if (!ready.WaitOne(5000)) throw new TimeoutException("Keyboard observer readiness was not confirmed.");
        if (!Ready) throw new InvalidOperationException("Keyboard observer hook or message pump did not become ready.");
    }

    private void Pump() {
        UIntPtr timer = UIntPtr.Zero;
        try {
            threadId = GetCurrentThreadId();
            Message message;
            PeekMessageW(out message, IntPtr.Zero, 0, 0, 0);
            if (stopping) return;
            hook = SetWindowsHookExW(13, callback, GetModuleHandleW(null), 0);
            if (hook == IntPtr.Zero) throw new System.ComponentModel.Win32Exception(Marshal.GetLastWin32Error(), "WH_KEYBOARD_LL install failed");
            timer = SetTimer(IntPtr.Zero, UIntPtr.Zero, 90000, IntPtr.Zero);
            if (timer == UIntPtr.Zero) throw new System.ComponentModel.Win32Exception(Marshal.GetLastWin32Error(), "Capture timer install failed");
            if (!PostThreadMessageW(threadId, ReadyMessage, UIntPtr.Zero, IntPtr.Zero))
                throw new System.ComponentModel.Win32Exception(Marshal.GetLastWin32Error(), "Readiness message failed");
            while (!stopping) {
                var result = GetMessageW(out message, IntPtr.Zero, 0, 0);
                if (result == -1) throw new System.ComponentModel.Win32Exception(Marshal.GetLastWin32Error(), "Observer GetMessage failed");
                if (result == 0) {
                    if (!stopping) errors.Enqueue("Observer message pump exited before fixture cleanup.");
                    break;
                }
                if (message.Id == ReadyMessage) {
                    Ready = true;
                    ready.Set();
                } else if (message.Id == 0x0113 && message.WParam == timer) {
                    errors.Enqueue("Capture expired at its 90-second diagnostic bound.");
                    break;
                } else {
                    TranslateMessage(ref message);
                    DispatchMessageW(ref message);
                }
            }
        } catch (Exception error) {
            errors.Enqueue(error.ToString());
        } finally {
            RemoveHook();
            if (timer != UIntPtr.Zero) KillTimer(IntPtr.Zero, timer);
            ready.Set();
            GC.KeepAlive(callback);
        }
    }

    public static bool Included(uint vk, uint scan) {
        return vk == 0x11 || vk == 0xA2 || vk == 0xA3 || vk == 0x4E ||
            (vk == 0xE7 && (scan == 0x6E || scan == 0x4E));
    }

    private IntPtr ObserveKey(int code, UIntPtr message, IntPtr data) {
        try {
            if (code == 0) {
                var key = Marshal.PtrToStructure<KeyboardInput>(data);
                if (Included(key.Vk, key.Scan)) {
                    if (count < Capacity) {
                        // Raw OS state only. No window text, menu queries, or file writes in the hook.
                        var row = new KeyRow {
                            Order = Interlocked.Increment(ref order), UtcMs = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds(),
                            MonotonicTicks = Stopwatch.GetTimestamp(), Message = (uint)message.ToUInt64(),
                            Vk = key.Vk, Scan = key.Scan, Flags = key.Flags, EventTimeMs = key.Time,
                            Injected = (key.Flags & 0x10) != 0, LowerIntegrityInjected = (key.Flags & 2) != 0,
                            Up = (key.Flags & 0x80) != 0, Context = ReadContext()
                        };
                        keys[count++] = row;
                    } else { overflow = true; }
                }
            }
        } catch (Exception error) { errors.Enqueue(error.ToString()); }
        return CallNextHookEx(IntPtr.Zero, code, message, data);
    }

    private static Context ReadContext() {
        var foreground = GetForegroundWindow();
        uint process;
        var foregroundThread = GetWindowThreadProcessId(foreground, out process);
        var info = new GuiThreadInfo { Size = (uint)Marshal.SizeOf(typeof(GuiThreadInfo)) };
        var available = GetGUIThreadInfo(foregroundThread, ref info);
        var error = available ? 0 : Marshal.GetLastWin32Error();
        uint focusProcess = 0;
        var focusThread = available && info.Focus != IntPtr.Zero ? GetWindowThreadProcessId(info.Focus, out focusProcess) : 0;
        return new Context {
            Foreground = foreground.ToInt64(), ForegroundThread = foregroundThread, ForegroundProcess = process,
            Focus = available ? info.Focus.ToInt64() : 0, FocusThread = focusThread, FocusProcess = focusProcess,
            FocusAvailable = available, FocusError = error, Layout = GetKeyboardLayout(foregroundThread).ToInt64(),
            AsyncModifiersBeforeUpdate = ((GetAsyncKeyState(0x11) & 0x8000) != 0 ? 1 : 0) |
                ((GetAsyncKeyState(0x10) & 0x8000) != 0 ? 2 : 0) | ((GetAsyncKeyState(0x12) & 0x8000) != 0 ? 4 : 0) |
                (((GetAsyncKeyState(0x5B) | GetAsyncKeyState(0x5C)) & 0x8000) != 0 ? 8 : 0)
        };
    }

    public void Mark(string name) {
        try {
            markers.Add(new {
                order = Interlocked.Increment(ref order), utcMs = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds(),
                monotonicTicks = Stopwatch.GetTimestamp(), name = name, context = ReadContext()
            });
        } catch (Exception error) { errors.Enqueue(error.ToString()); }
    }

    private void RemoveHook() {
        var installed = Interlocked.Exchange(ref hook, IntPtr.Zero);
        if (installed != IntPtr.Zero && !UnhookWindowsHookEx(installed)) {
            removalFailed = true;
            errors.Enqueue("UnhookWindowsHookEx failed with Win32 error " + Marshal.GetLastWin32Error());
        }
    }

    public void Stop() {
        stopping = true;
        if (thread != null && thread.IsAlive) {
            if (threadId != 0 && !PostThreadMessageW(threadId, 0x0012, UIntPtr.Zero, IntPtr.Zero))
                errors.Enqueue("Observer stop message failed with Win32 error " + Marshal.GetLastWin32Error());
            if (!thread.Join(5000)) {
                errors.Enqueue("Observer message-pump thread did not stop.");
                RemoveHook();
            }
        }
        if (thread == null || !thread.IsAlive) ready.Dispose();
    }

    public object Evidence() {
        var rows = new KeyRow[count];
        Array.Copy(keys, rows, count);
        return new {
            temporaryDiagnostic = true, ready = Ready, healthy = Healthy, observerThreadId = threadId,
            threadStopped = thread == null || !thread.IsAlive, hookRemoved = hook == IntPtr.Zero && !removalFailed,
            capacity = Capacity, overflow = overflow, errors = errors.ToArray(),
            semantics = new {
                order = "Shared sequence across filtered hook events and fixture markers. UTC milliseconds correlate with desktop-key-trace.",
                eventTimeMs = "KBDLLHOOKSTRUCT.time is a Windows message timestamp, not UTC.",
                monotonicTicks = "Stopwatch ticks in the fixture process.", monotonicFrequency = Stopwatch.Frequency,
                modifiers = "Bits Ctrl=1 Shift=2 Alt=4 Win=8. Hook async state is sampled BEFORE Windows updates it for this event. Marker state is sampled at the marker.",
                focus = "Raw foreground and GUI-thread focus HWND/PID/TID snapshots, not an atomic or guaranteed input destination. No window messages are sent.",
                packet = "Only VK_PACKET scan values U+006E and U+004E are retained. No arbitrary key text is decoded.",
                absence = "No event is not proof of WScript failure. Hooks can be silently removed or input suppressed before this observer. Ctrl events and both markers are the canary."
            },
            keys = rows, markers = markers.ToArray()
        };
    }
}
