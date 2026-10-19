using KVGuzzler.Core.Programs;
using Xunit;

namespace KVGuzzler.Tests;

public class ProgramLabelTests
{
    private static string Label(string program, string? commandLine = null, string? exe = null, string? parent = null) =>
        ProgramLabeler.Label(program, commandLine is null && exe is null ? null : new ProcessDetails(commandLine, exe, null), parent);

    [Theory]
    [InlineData("chrome.exe", false)]
    [InlineData("comfyui.exe", false)]
    [InlineData("python.exe", true)]
    [InlineData("python3.12.exe", true)]
    [InlineData("pythonw.exe", true)]
    [InlineData("node.exe", true)]
    [InlineData("JAVA.EXE", true)]
    public void OnlyInterpretersAreGeneric(string program, bool generic) =>
        Assert.Equal(generic, ProgramLabeler.IsGeneric(program));

    [Fact]
    public void OrdinaryProgramsKeepTheirName() =>
        Assert.Equal("chrome.exe", Label("chrome.exe", "chrome.exe --type=gpu"));

    [Theory]
    // a stock script name is replaced by the folder it is in
    [InlineData(@"python.exe C:\Users\me\ComfyUI\main.py --listen", "python.exe (ComfyUI)")]
    [InlineData(@"python.exe ""C:\Apps\Fancy App\resources\ComfyUI\main.py""", "python.exe (ComfyUI)")]
    // a descriptive script name is used as it is
    [InlineData(@"python.exe train_lora.py --epochs 3", "python.exe (train_lora)")]
    [InlineData(@"python.exe -u -X utf8 C:\tools\render_queue.py", "python.exe (render_queue)")]
    // modules, and inline code
    [InlineData(@"python.exe -m uvicorn app:main", "python.exe (uvicorn)")]
    [InlineData(@"python.exe -c ""print(1)""", "python.exe")]
    [InlineData(@"python.exe", "python.exe")]
    // other runtimes
    [InlineData(@"node.exe --max-old-space-size=4096 C:\proj\bot\server.js", "node.exe (bot)")]
    [InlineData(@"java.exe -Xmx2g -jar C:\games\minecraft-server.jar nogui", "java.exe (minecraft-server)")]
    [InlineData(@"java.exe -cp lib\* com.example.Worker", "java.exe (Worker)")]
    [InlineData(@"dotnet.exe C:\svc\Billing.Worker.dll", "dotnet.exe (Billing.Worker)")]
    [InlineData(@"pwsh.exe -NoProfile -File C:\ops\backup.ps1", "pwsh.exe (backup)")]
    [InlineData(@"pwsh.exe -NoProfile -Command Get-Date", "pwsh.exe")]
    public void NamesWhatTheCommandLineRuns(string commandLine, string expected)
    {
        var program = Path.GetFileName(ProgramLabeler.Tokenize(commandLine)[0]);
        Assert.Equal(expected, Label(program, commandLine));
    }

    [Fact]
    public void FallsBackToTheParentProgram()
    {
        Assert.Equal("python.exe (comfyui)", Label("python.exe", "python.exe", parent: "comfyui.exe"));
        Assert.Equal("python.exe (Code)", Label("python.exe", "python.exe", parent: "Code.exe"));
    }

    [Theory]
    [InlineData("explorer.exe")]
    [InlineData("cmd.exe")]
    [InlineData("pwsh.exe")]            // another interpreter says nothing either
    [InlineData("python.exe")]
    [InlineData(null)]
    public void AParentThatSaysNothingIsIgnored(string? parent) =>
        Assert.Equal("python.exe", Label("python.exe", "python.exe", parent: parent));

    [Fact]
    public void TheCommandLineBeatsTheParent() =>
        Assert.Equal("python.exe (ComfyUI)", Label("python.exe", @"python.exe D:\ComfyUI\main.py", parent: "launcher.exe"));

    [Theory]
    [InlineData(@"C:\Users\me\ComfyUI\.venv\Scripts\python.exe", "python.exe (ComfyUI)")]
    [InlineData(@"/home/me/ComfyUI/venv/bin/python.exe", "python.exe (ComfyUI)")]
    [InlineData(@"D:\ComfyUI_windows_portable\python_embeded\python.exe", "python.exe (ComfyUI_windows_portable)")]
    [InlineData(@"C:\Users\me\AppData\Local\Programs\Python\Python312\python.exe", "python.exe")]   // a system install names nothing
    [InlineData(@"C:\.venv\Scripts\python.exe", "python.exe")]                                        // the folder above is the drive
    public void FallsBackToTheEnvironmentItLivesIn(string exe, string expected) =>
        Assert.Equal(expected, Label("python.exe", null, exe));

    [Fact]
    public void LongLabelsAreShortened()
    {
        var label = Label("python.exe", "python.exe " + new string('x', 80) + ".py");
        Assert.True(label.Length < "python.exe ()".Length + 40);
        Assert.EndsWith("…)", label);
    }

    [Fact]
    public void NothingKnownKeepsThePlainName() => Assert.Equal("python.exe", Label("python.exe"));

    [Theory]
    [InlineData(@"a b c", new[] { "a", "b", "c" })]
    [InlineData(@"""C:\Program Files\x.exe"" --flag ""two words""", new[] { @"C:\Program Files\x.exe", "--flag", "two words" })]
    [InlineData(@"  spaced   out  ", new[] { "spaced", "out" })]
    [InlineData(@"a """" b", new[] { "a", "", "b" })]
    public void SplitsLikeWindows(string line, string[] expected) => Assert.Equal(expected, ProgramLabeler.Tokenize(line));
}

public class ProgramNamerTests
{
    private sealed class FakeInspector(Func<int, ProcessDetails?> lookup) : IProcessInspector
    {
        public List<int> Asked { get; } = new();
        public ProcessDetails? Inspect(int pid) { Asked.Add(pid); return lookup(pid); }
    }

    private static ProcessDetails Run(string commandLine, int? parent = null) => new(commandLine, null, parent);

    [Fact]
    public void OrdinaryProgramsAreNeverLookedUp()
    {
        var inspector = new FakeInspector(_ => throw new InvalidOperationException("should not be asked"));
        var namer = new ProgramNamer(inspector);
        Assert.Equal("chrome.exe", namer.Name(10, "chrome.exe", _ => null));
        Assert.Empty(inspector.Asked);
    }

    [Fact]
    public void AsksOncePerProcessAndRemembers()
    {
        var inspector = new FakeInspector(_ => Run(@"python.exe C:\ComfyUI\main.py"));
        var namer = new ProgramNamer(inspector);
        for (var i = 0; i < 5; i++) Assert.Equal("python.exe (ComfyUI)", namer.Name(42, "python.exe", _ => null));
        Assert.Single(inspector.Asked);
    }

    [Fact]
    public void ProcessesRunningDifferentThingsGetDifferentLabels()
    {
        var namer = new ProgramNamer(new FakeInspector(pid => Run(pid == 1 ? @"python.exe C:\a\ComfyUI\main.py" : @"python.exe C:\b\train.py")));
        Assert.Equal("python.exe (ComfyUI)", namer.Name(1, "python.exe", _ => null));
        Assert.Equal("python.exe (train)", namer.Name(2, "python.exe", _ => null));
    }

    [Fact]
    public void UsesTheParentsName()
    {
        var namer = new ProgramNamer(new FakeInspector(_ => Run("python.exe", parent: 7)));
        Assert.Equal("python.exe (comfyui)", namer.Name(1, "python.exe", pid => pid == 7 ? "comfyui.exe" : null));
    }

    [Fact]
    public void AProcessThatCannotBeReadKeepsItsNameAndIsNotRetried()
    {
        var inspector = new FakeInspector(_ => null);
        var namer = new ProgramNamer(inspector);
        Assert.Equal("python.exe", namer.Name(5, "python.exe", _ => null));
        Assert.Equal("python.exe", namer.Name(5, "python.exe", _ => null));
        Assert.Single(inspector.Asked);
    }

    [Fact]
    public void AnInspectorThatThrowsDoesNotBreakNaming() =>
        Assert.Equal("python.exe", new ProgramNamer(new FakeInspector(_ => throw new InvalidOperationException())).Name(5, "python.exe", _ => null));

    [Fact]
    public void NoInspectorMeansPlainNames() =>
        Assert.Equal("node.exe", new ProgramNamer(null).Name(5, "node.exe", _ => null));

    [Fact]
    public void AReusedProcessIdIsLookedUpAgain()
    {
        var inspector = new FakeInspector(_ => Run(@"node.exe C:\x\render.js"));
        var namer = new ProgramNamer(inspector);
        namer.Name(9, "python.exe", _ => null);
        Assert.Equal("node.exe (render)", namer.Name(9, "node.exe", _ => null));   // same id, different program
        Assert.Equal(2, inspector.Asked.Count);
    }

    [Fact]
    public void ForgetsProcessesThatAreGone()
    {
        var inspector = new FakeInspector(_ => Run(@"python.exe C:\x\render.py"));
        var namer = new ProgramNamer(inspector);
        namer.Name(9, "python.exe", _ => null);
        namer.Retain(new[] { 1, 2 });
        namer.Name(9, "python.exe", _ => null);
        Assert.Equal(2, inspector.Asked.Count);
    }
}

public class WindowsProcessInspectorTests
{
    [Fact]
    public void ReadsThisProcess()
    {
        if (!OperatingSystem.IsWindows()) return;
        var details = new WindowsProcessInspector().Inspect(Environment.ProcessId);
        Assert.NotNull(details);
        Assert.False(string.IsNullOrWhiteSpace(details.CommandLine));
        Assert.EndsWith(".exe", details.ExecutablePath, StringComparison.OrdinalIgnoreCase);
        Assert.True(details.ParentPid > 0);
    }

    [Fact]
    public void ANonexistentProcessIsNull()
    {
        if (!OperatingSystem.IsWindows()) return;
        Assert.Null(new WindowsProcessInspector().Inspect(int.MaxValue - 1));
    }
}
