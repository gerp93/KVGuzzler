using System.ComponentModel;
using System.Diagnostics;
using KVGuzzler.Core.History;

namespace KVGuzzler.Collector;

public enum ServiceState { NotInstalled, Stopped, Running, Other, Unsupported }

/// <summary>What the elevated copy of the program is asked to do to the collector service.</summary>
public enum ServiceChange { Install, Uninstall, StopForUpdate }

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
    public const string StopArgument = "--stop-service";

    public static string ArgumentFor(ServiceChange change) => change switch
    {
        ServiceChange.Install => InstallArgument,
        ServiceChange.Uninstall => UninstallArgument,
        _ => StopArgument,
    };

    /// <summary>The service's permissions: Windows' defaults, plus start and stop for the logged-in user. The program lives in the
    /// user's own folder and the service runs from it, so an update has to stop the service before it can replace those files; without
    /// this the update needs administrator rights and silently fails ("one or more running processes prevented it"). Changing what the
    /// service runs still needs administrator rights, and the user could already replace the program files the service runs.</summary>
    internal const string ServiceSecurity =
        "D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)(A;;CCLCSWRPWPLOCRRC;;;IU)(A;;CCLCSWLOCRRC;;;SU)";

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
        new[] { "sdset", name, ServiceSecurity },
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
    public static int RunElevated(ServiceChange change, bool development)
    {
        var dataDir = HistoryStore.DefaultDirectory(development);
        Directory.CreateDirectory(dataDir);
        var log = Path.Combine(dataDir, "service-install.log");

        try
        {
            var message = change switch
            {
                ServiceChange.Install => Install(development),
                ServiceChange.Uninstall => Uninstall(development),
                _ => StopForUpdate(development, elevated: true),
            };
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

    /// <summary>How long to wait for the service to finish stopping: its files stay locked until the process has exited.</summary>
    private static readonly TimeSpan StopTimeout = TimeSpan.FromSeconds(30);

    /// <summary>Stops the service and waits until it has, so its files can be replaced by an update. Returns a message saying
    /// what happened. As administrator it also makes sure the service's permissions let the user stop and start it, which a
    /// service installed by an older version lacks (so the next update needs no prompt).</summary>
    public static string StopForUpdate(bool development, bool elevated = false)
    {
        var name = ServiceName(development);
        if (Query(development) == ServiceState.NotInstalled) return $"Service {name} is not installed.";
        if (elevated) Sc("sdset", name, ServiceSecurity);
        if (Query(development) == ServiceState.Running) Sc("stop", name);

        var until = DateTime.UtcNow + StopTimeout;
        while (Query(development) is ServiceState.Running or ServiceState.Other && DateTime.UtcNow < until)
            Thread.Sleep(250);
        if (Query(development) is ServiceState.Running or ServiceState.Other)
            throw new InvalidOperationException($"Service {name} did not stop.");
        return $"Service {name} stopped.";
    }

    /// <summary>Gets the service out of the way of an update, trying first without administrator rights (works once the
    /// service's permissions allow the user to stop it) and then through Windows' permission prompt. Returns null when the files
    /// are free to replace, otherwise a sentence for the user.</summary>
    public static async Task<string?> EnsureStoppedForUpdateAsync(bool development)
    {
        if (Query(development) is ServiceState.NotInstalled or ServiceState.Stopped or ServiceState.Unsupported) return null;
        try
        {
            await Task.Run(() => StopForUpdate(development));
            return null;
        }
        catch (Exception)
        {
            // Not allowed to stop it (or it would not stop): ask Windows for permission.
        }

        var (started, message) = await RequestElevatedAsync(ServiceChange.StopForUpdate, development);
        if (!started) return "The update needs to pause background recording first, and Windows was not allowed to. Nothing was changed.";
        return message.Contains("FAILED", StringComparison.Ordinal) ? "Could not pause background recording for the update: " + message : null;
    }

    /// <summary>Starts it again after an update. Best effort.</summary>
    public static void StartAfterUpdate(bool development)
    {
        if (Query(development) == ServiceState.Stopped) Sc("start", ServiceName(development));
    }

    /// <summary>Starts an elevated copy of this program to install or remove the service. Windows shows
    /// its "allow this app to make changes to your device" question (UAC). Returns false if the user said no.</summary>
    public static async Task<(bool Started, string Message)> RequestElevatedAsync(ServiceChange change, bool development)
    {
        var info = new ProcessStartInfo(ExecutablePath, ArgumentFor(change))
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
