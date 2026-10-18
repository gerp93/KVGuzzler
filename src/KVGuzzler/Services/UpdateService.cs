using Velopack;
using Velopack.Sources;

namespace KVGuzzler.Services;

/// <summary>Self-update through Velopack, wired as dotnet-avalonia.md specifies. Only an installed
/// build can update: a dev run reports that instead of throwing.</summary>
public sealed class UpdateService
{
    private readonly UpdateManager _manager;
    private UpdateInfo? _pending;

    public UpdateService(string repoUrl) =>
        _manager = new UpdateManager(new GithubSource(repoUrl, accessToken: null, prerelease: false));

    public bool IsInstalled => _manager.IsInstalled;

    /// <summary>The installed version, taken from Velopack and nowhere else.</summary>
    public string VersionText => _manager.CurrentVersion?.ToString() ?? "development build";

    public bool UpdatePending => _pending is not null;

    public async Task<string> CheckAsync()
    {
        if (!IsInstalled) return "Update checks only run in an installed build (this is a development run).";
        try
        {
            _pending = await _manager.CheckForUpdatesAsync();
            return _pending is null
                ? "You are up to date."
                : $"Version {_pending.TargetFullRelease.Version} is available.";
        }
        catch (Exception e)
        {
            _pending = null;
            return "Could not check for updates: " + e.Message;
        }
    }

    /// <summary>Downloads the pending update and restarts into it. Does not return on success.</summary>
    public async Task DownloadAndRestartAsync(Action<int>? progress = null)
    {
        if (_pending is null) return;
        await _manager.DownloadUpdatesAsync(_pending, progress);
        _manager.ApplyUpdatesAndRestart(_pending);
    }
}
