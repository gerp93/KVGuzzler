using System.ComponentModel;
using System.Diagnostics;
using KVGuzzler.Core.History;

namespace KVGuzzler.Collector;

public enum ServiceState { NotInstalled, Stopped, Running, Other, Unsupported }

/// <summary>
/// Installs, removes and queries the collector's Windows service through <c>sc.exe</c>. Changing a service
/// needs administrator rights, so the window starts a second copy of itself elevated
/// (<c>--install-service</c>, which is what makes Windows show its "allow this app to make changes"
/// question) and reads the result from a log file, since a copy started that way has no console.
/// </summary>
public static class ServiceInstaller
{
    public const string InstallArgument = "--install-service";
    public const string UninstallArgument = "--uninstall-service";

    public static string ServiceName(bool development) => development ? "KVGuzzlerCollector-dev" : "KVGuzzlerCollector";

    /// <summary>The program the service runs: this same executable, in collector mode. (A development run
    /// can be hosted by <c>dotnet.exe</c>, so the executable is found next to the assembly instead.)</summary>
    public static string ExecutablePath => Path.Combine(AppContext.BaseDirectory, "KVGuzzler.exe");

    internal static string BinaryPathValue(string exePath) => $"\"{exePath}\" {CollectorHost.Argument}";

    /// <summary>The sc.exe argument lists that make the service exist with the right settings. Separate so
    /// the exact commands can be tested without administrator rights.</summary>
    internal static IReadOnlyList<string[]> CreateCommands(string name, string exePath, bool exists) => new[]
    {
        exists
            ? new[] { "config", name, "binPath=", BinaryPathValue(exePath), "start=", "auto" }
            : new[] { "create", name, "binPath=", BinaryPathValue(exePath), "start=", "auto", "DisplayName=", "KVGuzzler power collector" },
        new[] { "description", name, "Records this PC's CPU and GPU power use for KVGuzzler, even when nobody is logged in." },
        // If it ever dies, bring it back: after 5 s, 30 s, then a minute; forget the count after a day.
        new[] { "failure", name, "reset=", "86400", "actions=", "restart/5000/restart/30000/restart/60000" },
    };

    // ----- Query ------------------------------------------------------------------------

    public static ServiceState Query(bool development)
    {
        if (!OperatingSystem.IsWindows()) return ServiceState.Unsupported;
        var (code, output) = Sc("query", ServiceName(development));
        return ParseState(code, output);
    }

    /// <summary>Reads sc.exe's answer. Exit code 1060 means no such service; the state is the number after
    /// "STATE :" (4 running, 1 stopped), not the word, which Windows may translate.</summary>
    internal static ServiceState ParseState(int exitCode, string output)
    {
        if (exitCode != 0) return ServiceState.NotInstalled;
        var state = System.Text.RegularExpressions.Regex.Match(output, @"STATE\s*:\s*(\d+)");
        if (!state.Success) return ServiceState.Other;
        return state.Groups[1].Value switch { "4" => ServiceState.Running, "1" => ServiceState.Stopped, _ => ServiceState.Other };
    }

    // ----- Changes (administrator rights) -----------------------------------------------

    /// <summary>Entry point of the elevated copy: install or remove, and say what happened in the log.</summary>
    public static int RunElevated(bool install, bool development)
    {
        var dataDir = HistoryStore.DefaultDirectory(development);
        Directory.CreateDirectory(dataDir);
        var log = Path.Combine(dataDir, "service-install.log");

        try
        {
            var message = install ? Install(development) : Uninstall(development);
            File.WriteAllText(log, $"{DateTimeOffset.Now:u} OK: {message}{Environment.NewLine}");
            return 0;
        }
        catch (Exception e)
        {
            File.WriteAllText(log, $"{DateTimeOffset.Now:u} FAILED: {e.Message}{Environment.NewLine}");
            return 1;
        }
    }

    /// <summary>Creates (or repairs) the service and starts it. Safe to run again.</summary>
    public static string Install(bool development)
    {
        if (!OperatingSystem.IsWindows()) throw new PlatformNotSupportedException("The collector service is Windows only.");
        if (!File.Exists(ExecutablePath)) throw new FileNotFoundException("KVGuzzler.exe was not found next to the app.", ExecutablePath);

        var name = ServiceName(development);
        var exists = Query(development) != ServiceState.NotInstalled;
        foreach (var args in CreateCommands(name, ExecutablePath, exists)) ScOrThrow(args);

        if (Query(development) != ServiceState.Running)
        {
            var (code, output) = Sc("start", name);
            if (code != 0 && code != 1056)   // 1056: already running
                throw new InvalidOperationException($"The service was installed but did not start (sc.exe {code}): {output.Trim()}");
        }
        return $"Service {name} installed and running.";
    }

    public static string Uninstall(bool development)
    {
        var name = ServiceName(development);
        if (Query(development) == ServiceState.NotInstalled) return $"Service {name} was not installed.";
        Sc("stop", name);
        ScOrThrow(new[] { "delete", name });
        return $"Service {name} removed.";
    }

    /// <summary>Stops the service so its files can be replaced by an update. Best effort.</summary>
    public static void StopForUpdate(bool development)
    {
        if (Query(development) == ServiceState.Running) Sc("stop", ServiceName(development));
    }

    /// <summary>Starts it again after an update. Best effort.</summary>
    public static void StartAfterUpdate(bool development)
    {
        if (Query(development) == ServiceState.Stopped) Sc("start", ServiceName(development));
    }

    /// <summary>Starts an elevated copy of this program to install or remove the service. Windows shows
    /// its "allow this app to make changes to your device" question (UAC). Returns false if the user said no.</summary>
    public static async Task<(bool Started, string Message)> RequestElevatedAsync(bool install, bool development)
    {
        var info = new ProcessStartInfo(ExecutablePath, install ? InstallArgument : UninstallArgument)
        {
            UseShellExecute = true,
            Verb = "runas",
            WindowStyle = ProcessWindowStyle.Hidden,
        };

        try
        {
            using var process = Process.Start(info);
            if (process is not null) await process.WaitForExitAsync();
        }
        catch (Win32Exception e) when (e.NativeErrorCode == 1223)   // ERROR_CANCELLED
        {
            return (false, "Cancelled: Windows was not allowed to make the change.");
        }

        var log = Path.Combine(HistoryStore.DefaultDirectory(development), "service-install.log");
        return (true, File.Exists(log) ? File.ReadAllText(log).Trim() : "No result was recorded.");
    }

    // ----- sc.exe -----------------------------------------------------------------------

    private static void ScOrThrow(string[] args)
    {
        var (code, output) = Sc(args);
        if (code != 0) throw new InvalidOperationException($"sc.exe {args[0]} failed ({code}): {output.Trim()}");
    }

    private static (int ExitCode, string Output) Sc(params string[] args)
    {
        var info = new ProcessStartInfo("sc.exe")
        {
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            UseShellExecute = false,
            CreateNoWindow = true,
        };
        foreach (var a in args) info.ArgumentList.Add(a);

        using var process = Process.Start(info)!;
        var output = process.StandardOutput.ReadToEnd() + process.StandardError.ReadToEnd();
        process.WaitForExit();
        return (process.ExitCode, output);
    }
}
