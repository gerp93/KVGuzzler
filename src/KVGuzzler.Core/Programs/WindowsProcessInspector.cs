using System.Diagnostics;
using System.Runtime.InteropServices;
using System.Runtime.Versioning;
using System.Text;

namespace KVGuzzler.Core.Programs;

/// <summary>
/// Reads a process's command line, executable path and parent from Windows, with the least access
/// Windows offers (PROCESS_QUERY_LIMITED_INFORMATION): no administrator rights, and no elevated or other-user process
/// can be read, in which case that part is null.
/// </summary>
[SupportedOSPlatform("windows")]
public sealed class WindowsProcessInspector : IProcessInspector
{
    private const uint QueryLimited = 0x1000;
    private const int ProcessBasicInformationClass = 0;
    private const int ProcessCommandLineInformationClass = 60;   // Windows 8.1 and later

    public ProcessDetails? Inspect(int pid)
    {
        var handle = OpenProcess(QueryLimited, false, pid);
        if (handle == IntPtr.Zero) return null;
        try
        {
            return new ProcessDetails(ReadCommandLine(handle), ReadImagePath(handle), ReadParent(handle, pid));
        }
        finally { CloseHandle(handle); }
    }

    private static string? ReadCommandLine(IntPtr handle)
    {
        var size = 1024;
        for (var attempt = 0; attempt < 3; attempt++)
        {
            var buffer = Marshal.AllocHGlobal(size);
            try
            {
                var status = NtQueryInformationProcess(handle, ProcessCommandLineInformationClass, buffer, size, out var needed);
                if (status == 0)
                {
                    // A UNICODE_STRING: Length (bytes), MaximumLength, then the pointer (to text stored just after it).
                    var length = Marshal.ReadInt16(buffer);
                    var text = Marshal.ReadIntPtr(buffer, IntPtr.Size);
                    return length == 0 || text == IntPtr.Zero ? null : Marshal.PtrToStringUni(text, length / 2);
                }
                // STATUS_INFO_LENGTH_MISMATCH, STATUS_BUFFER_OVERFLOW, STATUS_BUFFER_TOO_SMALL: try again with the size it asked for.
                if (status is not (unchecked((int)0xC0000004) or unchecked((int)0x80000005) or unchecked((int)0xC0000023)) || needed <= size) return null;
                size = needed;
            }
            finally { Marshal.FreeHGlobal(buffer); }
        }
        return null;
    }

    private static string? ReadImagePath(IntPtr handle)
    {
        var text = new StringBuilder(1024);
        var length = text.Capacity;
        return QueryFullProcessImageName(handle, 0, text, ref length) ? text.ToString(0, length) : null;
    }

    private static int? ReadParent(IntPtr handle, int pid)
    {
        var info = new ProcessBasicInformation();
        if (NtQueryInformationProcess(handle, ProcessBasicInformationClass, ref info, Marshal.SizeOf<ProcessBasicInformation>(), out _) != 0) return null;
        var parent = (long)info.InheritedFromUniqueProcessId;
        if (parent <= 0 || parent > int.MaxValue) return null;

        // The parent's id may since have been given to an unrelated program: it only counts when that process started first.
        try
        {
            using var parentProcess = Process.GetProcessById((int)parent);
            using var self = Process.GetProcessById(pid);
            return parentProcess.StartTime <= self.StartTime ? (int)parent : null;
        }
        catch (ArgumentException) { return null; }          // the parent has exited
        catch (Exception) { return (int)parent; }           // start time not readable: trust the id
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct ProcessBasicInformation
    {
        public IntPtr ExitStatus;
        public IntPtr PebBaseAddress;
        public IntPtr AffinityMask;
        public IntPtr BasePriority;
        public IntPtr UniqueProcessId;
        public IntPtr InheritedFromUniqueProcessId;
    }

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern IntPtr OpenProcess(uint access, bool inheritHandle, int pid);

    [DllImport("kernel32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool CloseHandle(IntPtr handle);

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool QueryFullProcessImageName(IntPtr handle, int flags, StringBuilder name, ref int size);

    [DllImport("ntdll.dll")]
    private static extern int NtQueryInformationProcess(IntPtr handle, int infoClass, IntPtr info, int length, out int returned);

    [DllImport("ntdll.dll")]
    private static extern int NtQueryInformationProcess(IntPtr handle, int infoClass, ref ProcessBasicInformation info, int length, out int returned);
}
