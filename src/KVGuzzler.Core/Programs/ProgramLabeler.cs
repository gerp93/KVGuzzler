using System.Text.RegularExpressions;

namespace KVGuzzler.Core.Programs;

/// <summary>What the operating system can say about one process beyond its name. Any part may be missing
/// (a protected process, or one that exited a moment ago).</summary>
public sealed record ProcessDetails(string? CommandLine, string? ExecutablePath, int? ParentPid);

/// <summary>Looks up <see cref="ProcessDetails"/> for a process id. Implementations return null rather than throw.</summary>
public interface IProcessInspector
{
    ProcessDetails? Inspect(int pid);
}

/// <summary>
/// Names a program for the per-program lists. Most programs are named by their executable (<c>chrome.exe</c>),
/// but an interpreter such as <c>python.exe</c> is one executable running thousands of different programs, so
/// on its own the name says nothing. For those generic names only, the label adds what is being run, found
/// from the command line (the script, module or jar), else the program that started it, else the virtual
/// environment it lives in: <c>python.exe (ComfyUI)</c>. Only a short label is kept; the command line itself is
/// never stored, because arguments can hold secrets.
/// </summary>
public static partial class ProgramLabeler
{
    private const int MaxLabel = 32;

    [GeneratedRegex(@"^python[\d.]*w?\.exe$", RegexOptions.IgnoreCase)]
    private static partial Regex PythonName();

    private static readonly HashSet<string> Interpreters = new(StringComparer.OrdinalIgnoreCase)
    {
        "pythonw.exe", "py.exe", "node.exe", "bun.exe", "deno.exe", "java.exe", "javaw.exe", "dotnet.exe",
        "pwsh.exe", "powershell.exe", "ruby.exe", "electron.exe",
    };

    /// <summary>Programs that start other programs without saying anything about them.</summary>
    private static readonly HashSet<string> Launchers = new(StringComparer.OrdinalIgnoreCase)
    {
        "explorer.exe", "cmd.exe", "conhost.exe", "openconsole.exe", "windowsterminal.exe", "wt.exe",
        "bash.exe", "sh.exe", "wsl.exe", "wslhost.exe", "services.exe", "svchost.exe", "wininit.exe",
        "winlogon.exe", "userinit.exe", "taskeng.exe", "taskhostw.exe", "kvguzzler.exe",
    };

    /// <summary>Script names that tell nothing about what the program is; the folder holding it does.</summary>
    private static readonly HashSet<string> GenericScripts = new(StringComparer.OrdinalIgnoreCase)
    {
        "main", "app", "server", "run", "start", "serve", "index", "cli", "__main__", "__init__", "manage",
        "launch", "launcher", "entry", "bootstrap", "wsgi", "asgi", "service", "daemon",
    };

    private static readonly HashSet<string> GenericFolders = new(StringComparer.OrdinalIgnoreCase)
    {
        "src", "source", "bin", "scripts", "script", "lib", "dist", "build", "app", "apps", "resources",
        "out", "release", "debug", "current", "server", "backend", "core", "python", "py", "main",
    };

    private static readonly HashSet<string> VenvFolders = new(StringComparer.OrdinalIgnoreCase)
    {
        ".venv", "venv", "env", ".env",
    };

    /// <summary>True when the executable name does not say what the program is (an interpreter or runtime).</summary>
    public static bool IsGeneric(string program) => Interpreters.Contains(program) || PythonName().IsMatch(program);

    /// <summary>The program's display name. <paramref name="parentProgram"/> is the (unlabelled) name of the process that
    /// started it, when known.</summary>
    public static string Label(string program, ProcessDetails? details, string? parentProgram)
    {
        if (!IsGeneric(program)) return program;
        var what = FromCommandLine(program, details?.CommandLine)
                   ?? FromParent(parentProgram)
                   ?? FromEnvironment(details?.ExecutablePath);
        return what is null ? program : $"{program} ({what})";
    }

    private static string? FromCommandLine(string program, string? commandLine)
    {
        if (string.IsNullOrWhiteSpace(commandLine)) return null;
        var args = Tokenize(commandLine).Skip(1).ToList();
        var lower = program.ToLowerInvariant();
        return lower switch
        {
            _ when PythonName().IsMatch(program) => PythonTarget(args),
            "java.exe" or "javaw.exe" => JavaTarget(args),
            "dotnet.exe" => args.Where(a => a.EndsWith(".dll", StringComparison.OrdinalIgnoreCase)).Select(ScriptLabel).FirstOrDefault(),
            "pwsh.exe" or "powershell.exe" => PowerShellTarget(args),
            _ => args.Where(a => !a.StartsWith('-') && a is not ("run" or "." or "..")).Select(ScriptLabel).FirstOrDefault(),
        };
    }

    private static string? PythonTarget(List<string> args)
    {
        for (var i = 0; i < args.Count; i++)
        {
            var a = args[i];
            if (a == "-m") return i + 1 < args.Count ? Tidy(args[i + 1]) : null;
            if (a == "-c") return null;                                   // inline code: nothing to name
            if (a is "-W" or "-X" or "-Q") { i++; continue; }              // flags that take a value
            if (a.StartsWith('-')) continue;
            return ScriptLabel(a);
        }
        return null;
    }

    private static string? JavaTarget(List<string> args)
    {
        for (var i = 0; i < args.Count; i++)
        {
            var a = args[i];
            if (a == "-jar") return i + 1 < args.Count ? ScriptLabel(args[i + 1]) : null;
            if (a is "-cp" or "-classpath" or "--class-path") { i++; continue; }
            if (a.StartsWith('-')) continue;
            return Tidy(a.Split('.').Last());                              // main class: com.example.App -> App
        }
        return null;
    }

    private static string? PowerShellTarget(List<string> args)
    {
        for (var i = 0; i + 1 < args.Count; i++)
            if (args[i].Equals("-File", StringComparison.OrdinalIgnoreCase)) return ScriptLabel(args[i + 1]);
        return null;
    }

    /// <summary>A label for a script path: its file name, or its folder's name when the file name is a stock one
    /// like <c>main.py</c>.</summary>
    private static string? ScriptLabel(string path)
    {
        var parts = path.Split('\\', '/').Where(p => p.Length > 0 && !p.EndsWith(':') && p is not ("." or "..")).ToList();
        if (parts.Count == 0) return null;
        var name = Path.GetFileNameWithoutExtension(parts[^1]);
        if (name.Length == 0) return null;
        if (!GenericScripts.Contains(name)) return Tidy(name);
        for (var i = parts.Count - 2; i >= 0 && i >= parts.Count - 4; i--)
            if (!GenericFolders.Contains(parts[i])) return Tidy(parts[i]);
        return null;
    }

    private static string? FromParent(string? parentProgram)
    {
        if (string.IsNullOrEmpty(parentProgram) || IsGeneric(parentProgram) || Launchers.Contains(parentProgram)) return null;
        return Tidy(Path.GetFileNameWithoutExtension(parentProgram));
    }

    /// <summary>A virtual environment or embedded Python names its project: <c>C:\ComfyUI\.venv\Scripts\python.exe</c>.</summary>
    private static string? FromEnvironment(string? exePath)
    {
        if (string.IsNullOrEmpty(exePath)) return null;
        var parts = exePath.Split('\\', '/').Where(p => p.Length > 0).ToList();
        // Walk up from python.exe: Scripts\ (or bin\) inside a venv folder inside the project, or an embedded Python folder inside the project.
        if (parts.Count >= 4 && parts[^2] is var dir && (dir.Equals("scripts", StringComparison.OrdinalIgnoreCase) || dir.Equals("bin", StringComparison.OrdinalIgnoreCase))
            && VenvFolders.Contains(parts[^3]) && !parts[^4].EndsWith(':'))
            return Tidy(parts[^4]);
        if (parts.Count >= 3 && parts[^2].StartsWith("python_embed", StringComparison.OrdinalIgnoreCase) && !parts[^3].EndsWith(':'))
            return Tidy(parts[^3]);
        return null;
    }

    private static string? Tidy(string label)
    {
        label = label.Trim();
        if (label.Length == 0) return null;
        return label.Length <= MaxLabel ? label : label[..(MaxLabel - 1)] + "…";
    }

    /// <summary>Splits a Windows command line into its arguments, honouring double quotes.</summary>
    internal static List<string> Tokenize(string commandLine)
    {
        var tokens = new List<string>();
        var current = new System.Text.StringBuilder();
        var inQuotes = false;
        var started = false;
        foreach (var c in commandLine)
        {
            if (c == '"') { inQuotes = !inQuotes; started = true; }
            else if (char.IsWhiteSpace(c) && !inQuotes)
            {
                if (started) { tokens.Add(current.ToString()); current.Clear(); started = false; }
            }
            else { current.Append(c); started = true; }
        }
        if (started) tokens.Add(current.ToString());
        return tokens;
    }
}
