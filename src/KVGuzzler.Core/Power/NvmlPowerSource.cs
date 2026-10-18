using System.Runtime.InteropServices;
using System.Text;

namespace KVGuzzler.Core.Power;

/// <summary>The few NVML calls the app needs, so the source can be tested without a GPU.</summary>
public interface INvmlApi : IDisposable
{
    int DeviceCount { get; }
    string DeviceName(int index);

    /// <summary>Board power in milliwatts; null when the device does not report power.</summary>
    uint? PowerMilliwatts(int index);
}

/// <summary>
/// NVIDIA GPU board power through NVML, the library the driver installs (nvml.dll on Windows,
/// libnvidia-ml.so.1 on Linux), the same one nvidia-smi uses. No admin rights are needed.
/// Reports one <see cref="ReadingScope.Component"/> per GPU.
/// </summary>
public sealed class NvmlPowerSource : IPowerSource
{
    private readonly INvmlApi? _api;
    private readonly string[] _names = Array.Empty<string>();

    public NvmlPowerSource() : this(NativeNvml.TryOpen(out var why), why) { }

    public NvmlPowerSource(INvmlApi? api, string? unavailableReason = null)
    {
        _api = api;
        if (api is null)
        {
            UnavailableReason = unavailableReason ?? "NVML is not available.";
            return;
        }

        try
        {
            _names = Enumerable.Range(0, api.DeviceCount).Select(api.DeviceName).ToArray();
            if (_names.Length == 0) UnavailableReason = "No NVIDIA GPU found.";
        }
        catch (Exception e)
        {
            UnavailableReason = "Could not list NVIDIA GPUs: " + e.Message;
        }
    }

    public string Name => "NVIDIA GPU (NVML)";
    public string? UnavailableReason { get; }

    public IReadOnlyList<PowerReading> Read()
    {
        var readings = new List<PowerReading>();
        for (var i = 0; i < _names.Length; i++)
        {
            if (_api!.PowerMilliwatts(i) is { } mw)
                readings.Add(new PowerReading(_names[i], mw / 1000.0, ReadingScope.Component, PowerKind.Gpu));
        }
        return readings;
    }

    public void Dispose() => _api?.Dispose();
}

/// <summary>NVML loaded at run time: a machine without an NVIDIA driver must start fine, so there is
/// no compile-time DllImport that would fail to bind.</summary>
internal sealed class NativeNvml : INvmlApi
{
    private const int Success = 0;
    private const int NotSupported = 3;

    [UnmanagedFunctionPointer(CallingConvention.Cdecl)] private delegate int InitFn();
    [UnmanagedFunctionPointer(CallingConvention.Cdecl)] private delegate int ShutdownFn();
    [UnmanagedFunctionPointer(CallingConvention.Cdecl)] private delegate int CountFn(out uint count);
    [UnmanagedFunctionPointer(CallingConvention.Cdecl)] private delegate int HandleFn(uint index, out IntPtr device);
    [UnmanagedFunctionPointer(CallingConvention.Cdecl)] private delegate int NameFn(IntPtr device, byte[] name, uint length);
    [UnmanagedFunctionPointer(CallingConvention.Cdecl)] private delegate int PowerFn(IntPtr device, out uint milliwatts);

    private readonly IntPtr _lib;
    private readonly ShutdownFn _shutdown;
    private readonly CountFn _count;
    private readonly HandleFn _handle;
    private readonly NameFn _name;
    private readonly PowerFn _power;
    private IntPtr[] _devices = Array.Empty<IntPtr>();

    private NativeNvml(IntPtr lib)
    {
        _lib = lib;
        _shutdown = Bind<ShutdownFn>("nvmlShutdown");
        _count = Bind<CountFn>("nvmlDeviceGetCount_v2");
        _handle = Bind<HandleFn>("nvmlDeviceGetHandleByIndex_v2");
        _name = Bind<NameFn>("nvmlDeviceGetName");
        _power = Bind<PowerFn>("nvmlDeviceGetPowerUsage");
    }

    private static readonly string[] LibraryNames =
    {
        "nvml.dll",                                                         // Windows: System32, installed with the driver
        @"C:\Program Files\NVIDIA Corporation\NVSMI\nvml.dll",              // Windows: older driver layout
        "libnvidia-ml.so.1",                                                // Linux
    };

    public static INvmlApi? TryOpen(out string? unavailableReason)
    {
        IntPtr lib = IntPtr.Zero;
        foreach (var name in LibraryNames)
        {
            if (NativeLibrary.TryLoad(name, out lib)) break;
            lib = IntPtr.Zero;
        }
        if (lib == IntPtr.Zero)
        {
            unavailableReason = "No NVIDIA driver (NVML) found.";
            return null;
        }

        try
        {
            var init = (InitFn)Marshal.GetDelegateForFunctionPointer(
                NativeLibrary.GetExport(lib, "nvmlInit_v2"), typeof(InitFn));
            var rc = init();
            if (rc != Success)
            {
                NativeLibrary.Free(lib);
                unavailableReason = $"NVML failed to start (error {rc}).";
                return null;
            }

            var api = new NativeNvml(lib);
            api.LoadDevices();
            unavailableReason = null;
            return api;
        }
        catch (Exception e) when (e is EntryPointNotFoundException or DllNotFoundException or ArgumentException)
        {
            NativeLibrary.Free(lib);
            unavailableReason = "The NVIDIA driver's NVML is too old or damaged: " + e.Message;
            return null;
        }
    }

    private T Bind<T>(string export) where T : Delegate =>
        (T)Marshal.GetDelegateForFunctionPointer(NativeLibrary.GetExport(_lib, export), typeof(T));

    private void LoadDevices()
    {
        Check(_count(out var n), "count GPUs");
        _devices = new IntPtr[n];
        for (uint i = 0; i < n; i++) Check(_handle(i, out _devices[i]), "open GPU " + i);
    }

    public int DeviceCount => _devices.Length;

    public string DeviceName(int index)
    {
        var buffer = new byte[96];
        Check(_name(_devices[index], buffer, (uint)buffer.Length), "read the GPU name");
        var end = Array.IndexOf(buffer, (byte)0);
        return Encoding.ASCII.GetString(buffer, 0, end < 0 ? buffer.Length : end);
    }

    public uint? PowerMilliwatts(int index)
    {
        var rc = _power(_devices[index], out var mw);
        if (rc == NotSupported) return null;
        Check(rc, "read GPU power");
        return mw;
    }

    private static void Check(int rc, string what)
    {
        if (rc != Success) throw new InvalidOperationException($"NVML could not {what} (error {rc}).");
    }

    public void Dispose()
    {
        _shutdown();
        NativeLibrary.Free(_lib);
    }
}
